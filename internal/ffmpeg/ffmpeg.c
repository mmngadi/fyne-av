#include "ffmpeg.h"

#include <libavcodec/avcodec.h>
#include <libavformat/avformat.h>
#include <libavutil/imgutils.h>
#include <libavutil/opt.h>
#include <libavutil/pixdesc.h>
#include <libavutil/time.h>
#include <libswresample/swresample.h>
#include <libswscale/swscale.h>

#include <stdlib.h>
#include <string.h>

#define AV_MODE_AUTO 0
#define AV_MODE_VA 1
#define AV_MODE_V  2
#define AV_MODE_A  3

struct AVPlayer {
    AVFormatContext *fmt;
    AVCodecContext  *vctx;
    AVCodecContext  *actx;
    struct SwsContext *sws;
    SwrContext      *swr;

    int vstream;
    int astream;
    int has_video;
    int has_audio;
    int want_video;
    int want_audio;

    int64_t duration_ms;

    uint8_t *rgba_buf;
    int      rgba_size;
    int16_t *pcm_buf;
    int      pcm_size;
    int      pcm_cap;

    int eof;
};

static int open_codec(AVFormatContext *fmt, int stream_index, AVCodecContext **out) {
    const AVCodec *codec = avcodec_find_decoder(fmt->streams[stream_index]->codecpar->codec_id);
    if (!codec) return -1;
    AVCodecContext *ctx = avcodec_alloc_context3(codec);
    if (!ctx) return -1;
    if (avcodec_parameters_to_context(ctx, fmt->streams[stream_index]->codecpar) < 0) {
        avcodec_free_context(&ctx);
        return -1;
    }
    if (avcodec_open2(ctx, codec, NULL) < 0) {
        avcodec_free_context(&ctx);
        return -1;
    }
    *out = ctx;
    return 0;
}

static int64_t ts_to_ms(int64_t pts, AVStream *st) {
    if (pts == AV_NOPTS_VALUE) return -1;
    return av_rescale_q(pts, st->time_base, (AVRational){1, 1000});
}

AVPlayer *av_player_open(const char *url, int mode, char **err) {
    AVPlayer *p = (AVPlayer *)calloc(1, sizeof(*p));
    if (!p) return NULL;

    if (avformat_open_input(&p->fmt, url, NULL, NULL) < 0) {
        if (err) *err = strdup("avformat_open_input failed");
        goto fail;
    }
    if (avformat_find_stream_info(p->fmt, NULL) < 0) {
        if (err) *err = strdup("avformat_find_stream_info failed");
        goto fail;
    }

    p->vstream = -1;
    p->astream = -1;
    for (unsigned i = 0; i < p->fmt->nb_streams; i++) {
        enum AVMediaType t = p->fmt->streams[i]->codecpar->codec_type;
        if (t == AVMEDIA_TYPE_VIDEO && p->vstream < 0) p->vstream = (int)i;
        if (t == AVMEDIA_TYPE_AUDIO && p->astream < 0) p->astream = (int)i;
    }
    p->has_video = p->vstream >= 0;
    p->has_audio = p->astream >= 0;

    switch (mode) {
        case AV_MODE_VA: p->want_video = 1; p->want_audio = 1; break;
        case AV_MODE_V:  p->want_video = 1; p->want_audio = 0; break;
        case AV_MODE_A:  p->want_video = 0; p->want_audio = 1; break;
        default:
            p->want_video = p->has_video;
            p->want_audio = p->has_audio;
            break;
    }

    if (p->want_video && p->has_video) {
        if (open_codec(p->fmt, p->vstream, &p->vctx) < 0) {
            p->want_video = 0;
        }
    }
    if (p->want_audio && p->has_audio) {
        if (open_codec(p->fmt, p->astream, &p->actx) < 0) {
            p->want_audio = 0;
        }
    }
    if (!p->want_video && !p->want_audio) {
        if (err) *err = strdup("no decodable streams");
        goto fail;
    }

    if (p->fmt->duration != AV_NOPTS_VALUE && p->fmt->duration > 0) {
        p->duration_ms = p->fmt->duration / (AV_TIME_BASE / 1000);
    }
    return p;

fail:
    av_player_close(p);
    return NULL;
}

void av_player_close(AVPlayer *p) {
    if (!p) return;
    if (p->sws) sws_freeContext(p->sws);
    if (p->swr) swr_free(&p->swr);
    if (p->vctx) avcodec_free_context(&p->vctx);
    if (p->actx) avcodec_free_context(&p->actx);
    if (p->fmt) avformat_close_input(&p->fmt);
    free(p->rgba_buf);
    free(p->pcm_buf);
    free(p);
}

static int decode_video(AVPlayer *p, AVFrame *frame, AVFrameResult *out) {
    int w = frame->width, h = frame->height;
    int need = w * h * 4;
    if (need > p->rgba_size || !p->rgba_buf) {
        free(p->rgba_buf);
        p->rgba_buf = (uint8_t *)malloc(need);
        p->rgba_size = need;
    }
    if (!p->rgba_buf) return -1;

    p->sws = sws_getCachedContext(p->sws, w, h, frame->format,
        w, h, AV_PIX_FMT_RGBA, SWS_BILINEAR, NULL, NULL, NULL);
    if (!p->sws) return -1;

    uint8_t *dst[4] = { p->rgba_buf, NULL, NULL, NULL };
    int linesize[4] = { w * 4, 0, 0, 0 };
    const uint8_t *const src[4] = { frame->data[0], frame->data[1], frame->data[2], frame->data[3] };
    sws_scale(p->sws, src, frame->linesize, 0, h, dst, linesize);

    out->kind = AV_FRAME_VIDEO;
    out->video.data = p->rgba_buf;
    out->video.width = w;
    out->video.height = h;
    out->video.pts_ms = ts_to_ms(frame->pts, p->fmt->streams[p->vstream]);
    return 0;
}

// Fixed output audio format — must match the Go oto context
// (44100 Hz, stereo, S16 interleaved).
#define OUT_SAMPLE_RATE 44100
#define OUT_CHANNELS    2

static int decode_audio(AVPlayer *p, AVFrame *frame, AVFrameResult *out) {
    int in_ch = frame->ch_layout.nb_channels;
    if (in_ch <= 0) in_ch = 2;
    int in_sr = frame->sample_rate;
    if (in_sr <= 0) in_sr = 44100;

    // Allocate for the worst case after rate conversion.
    int max_out_samples = (int)av_rescale_rnd(frame->nb_samples, OUT_SAMPLE_RATE, in_sr, AV_ROUND_UP) + 1;
    int need_bytes = max_out_samples * OUT_CHANNELS * 2;
    if (need_bytes > p->pcm_cap || !p->pcm_buf) {
        free(p->pcm_buf);
        p->pcm_buf = (int16_t *)malloc(need_bytes);
        p->pcm_cap = need_bytes;
    }
    if (!p->pcm_buf) return -1;

    if (!p->swr) {
        struct AVChannelLayout out_ch;
        av_channel_layout_default(&out_ch, OUT_CHANNELS);
        swr_alloc_set_opts2(&p->swr,
            &out_ch, AV_SAMPLE_FMT_S16, OUT_SAMPLE_RATE,
            &frame->ch_layout, frame->format, in_sr, 0, NULL);
        if (swr_init(p->swr) < 0) {
            swr_free(&p->swr);
            return -1;
        }
    }
    // swr_convert may buffer internally across frames with rate changes;
    // loop until we get at least some output or input is exhausted.
    int converted = swr_convert(p->swr,
        (uint8_t **)&p->pcm_buf, max_out_samples,
        (const uint8_t **)frame->data, frame->nb_samples);
    if (converted < 0) return -1;

    out->kind = AV_FRAME_AUDIO;
    out->audio.data = p->pcm_buf;
    out->audio.n_samples = converted;
    out->audio.channels = OUT_CHANNELS;
    out->audio.sample_rate = OUT_SAMPLE_RATE;
    out->audio.pts_ms = ts_to_ms(frame->pts, p->fmt->streams[p->astream]);
    return 0;
}

int av_player_next_frame(AVPlayer *p, AVFrameResult *out) {
    if (!p || !out) return -1;
    out->kind = AV_FRAME_NONE;
    out->video.data = NULL;
    out->audio.data = NULL;

    if (p->eof) return AV_FRAME_EOF;

    AVFrame *frame = av_frame_alloc();
    AVPacket *pkt = av_packet_alloc();
    if (!frame || !pkt) {
        av_frame_free(&frame);
        av_packet_free(&pkt);
        return -1;
    }

    for (;;) {
        if (p->want_video) {
            int ret = avcodec_receive_frame(p->vctx, frame);
            if (ret == 0) {
                decode_video(p, frame, out);
                av_frame_unref(frame);
                av_frame_free(&frame);
                av_packet_free(&pkt);
                return 0;
            }
        }
        if (p->want_audio) {
            int ret = (p->actx) ? avcodec_receive_frame(p->actx, frame) : AVERROR(EAGAIN);
            if (ret == 0) {
                decode_audio(p, frame, out);
                av_frame_unref(frame);
                av_frame_free(&frame);
                av_packet_free(&pkt);
                return 0;
            }
        }

        int ret = av_read_frame(p->fmt, pkt);
        if (ret < 0) {
            // flush decoders
            if (p->vctx) avcodec_send_packet(p->vctx, NULL);
            if (p->actx) avcodec_send_packet(p->actx, NULL);
            // drain remaining frames
            if (p->want_video) {
                int r2 = avcodec_receive_frame(p->vctx, frame);
                if (r2 == 0) {
                    decode_video(p, frame, out);
                    av_frame_unref(frame);
                    av_frame_free(&frame);
                    av_packet_free(&pkt);
                    return 0;
                }
            }
            if (p->want_audio && p->actx) {
                int r2 = avcodec_receive_frame(p->actx, frame);
                if (r2 == 0) {
                    decode_audio(p, frame, out);
                    av_frame_unref(frame);
                    av_frame_free(&frame);
                    av_packet_free(&pkt);
                    return 0;
                }
            }
            p->eof = 1;
            av_frame_free(&frame);
            av_packet_free(&pkt);
            out->kind = AV_FRAME_EOF;
            return AV_FRAME_EOF;
        }

        if (pkt->stream_index == p->vstream && p->want_video) {
            avcodec_send_packet(p->vctx, pkt);
        } else if (pkt->stream_index == p->astream && p->want_audio && p->actx) {
            avcodec_send_packet(p->actx, pkt);
        }
        av_packet_unref(pkt);
    }
}

int av_player_seek(AVPlayer *p, int64_t target_ms) {
    if (!p || !p->fmt) return -1;
    int stream = p->vstream >= 0 ? p->vstream : p->astream;
    if (stream < 0) return -1;
    int64_t ts = av_rescale_q(target_ms, (AVRational){1,1000}, p->fmt->streams[stream]->time_base);
    int ret = av_seek_frame(p->fmt, stream, ts, AVSEEK_FLAG_BACKWARD);
    if (ret >= 0) p->eof = 0;
    return ret;
}

void av_player_flush(AVPlayer *p) {
    if (!p) return;
    if (p->vctx) avcodec_flush_buffers(p->vctx);
    if (p->actx) avcodec_flush_buffers(p->actx);
    p->eof = 0;
}

int64_t av_player_duration_ms(AVPlayer *p) {
    if (!p) return 0;
    return p->duration_ms;
}

int av_player_has_video(AVPlayer *p) { return p ? p->want_video : 0; }
int av_player_has_audio(AVPlayer *p) { return p ? p->want_audio : 0; }