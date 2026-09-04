#ifndef FYNE_AV_FFMPEG_H
#define FYNE_AV_FFMPEG_H

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

typedef struct AVPlayer AVPlayer;

typedef enum {
    AV_FRAME_NONE = 0,
    AV_FRAME_VIDEO = 1,
    AV_FRAME_AUDIO = 2,
    AV_FRAME_EOF  = 3,
    AV_FRAME_ERROR = -1
} AVFrameType;

typedef struct {
    uint8_t *data;
    int      width;
    int      height;
    int64_t  pts_ms;
} AVVideoInfo;

typedef struct {
    int16_t *data;
    int      n_samples;
    int      channels;
    int      sample_rate;
    int64_t  pts_ms;
} AVAudioInfo;

typedef struct {
    AVFrameType  kind;
    AVVideoInfo  video;
    AVAudioInfo  audio;
} AVFrameResult;

AVPlayer *av_player_open(const char *url, int mode, char **err);
void      av_player_close(AVPlayer *p);

int  av_player_next_frame(AVPlayer *p, AVFrameResult *out);

int      av_player_seek(AVPlayer *p, int64_t target_ms);
void     av_player_flush(AVPlayer *p);
int64_t  av_player_duration_ms(AVPlayer *p);
int      av_player_has_video(AVPlayer *p);
int      av_player_has_audio(AVPlayer *p);

#ifdef __cplusplus
}
#endif

#endif