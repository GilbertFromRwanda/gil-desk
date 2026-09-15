#ifndef NEXDESK_CODEC_H
#define NEXDESK_CODEC_H

#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

/* Stable C ABI status codes (planner Section 22). Ownership convention for
 * every pointer in this header: "borrowed" means the callee does not free
 * it; "owned" means the caller owns the returned memory and must call the
 * matching `_free` function. */
typedef enum {
    ND_CODEC_OK = 0,
    ND_CODEC_ERR_INVALID_ARG = 1,
    ND_CODEC_ERR_NO_MEMORY = 2,
    ND_CODEC_ERR_ENCODE = 3,
    ND_CODEC_ERR_DECODE = 4,
    ND_CODEC_ERR_UNSUPPORTED = 5,
    ND_CODEC_ERR_INTERNAL = 6
} nd_codec_status;

/* ABI version check, matches core/ffi's nd_ffi_abi_version(). */
unsigned int nd_codec_abi_version(void);

/* Encoder wraps libx264; decoder wraps libavcodec/FFmpeg's H.264 decoder.
 * Neither implements any codec logic itself (planner Section 2/6 decision:
 * wrap existing libraries, never hand-roll a codec). Both operate on
 * planar I420 (YUV 4:2:0) frames. */

typedef struct nd_encoder nd_encoder_t; /* opaque */
typedef struct nd_decoder nd_decoder_t; /* opaque */

/* owned: caller must call nd_encoder_destroy. Returns NULL on failure. */
nd_encoder_t *nd_encoder_create(int width, int height, int fps, int bitrate_kbps);

/*
 * Encodes one I420 frame (borrowed: y/u/v planes, not retained after this
 * call). On ND_CODEC_OK, *out_data is owned by the caller (free with
 * nd_buffer_free) and *out_len is its length; the encoder may buffer
 * frames internally, in which case *out_len is 0 and *out_data is NULL
 * (not an error) until enough frames have been fed in.
 */
nd_codec_status nd_encoder_encode(
    nd_encoder_t *encoder,
    const uint8_t *y, int y_stride,
    const uint8_t *u, int u_stride,
    const uint8_t *v, int v_stride,
    uint8_t **out_data, size_t *out_len);

void nd_encoder_destroy(nd_encoder_t *encoder);

/* owned: caller must call nd_decoder_destroy. Returns NULL on failure. */
nd_decoder_t *nd_decoder_create(void);

/*
 * Decodes one Annex B H.264 access unit (borrowed: `data`). On
 * ND_CODEC_OK with *out_len > 0, *out_data is an owned I420 buffer (free
 * with nd_buffer_free) of size *out_width * *out_height * 3 / 2, planes
 * laid out Y then U then V, each tightly packed (stride == width, or
 * width/2 for U/V). A decoder may need more input before it can produce
 * a frame, in which case *out_len is 0 and *out_data is NULL — not an
 * error.
 */
nd_codec_status nd_decoder_decode(
    nd_decoder_t *decoder,
    const uint8_t *data, size_t len,
    uint8_t **out_data, size_t *out_len,
    int *out_width, int *out_height);

void nd_decoder_destroy(nd_decoder_t *decoder);

/* Frees a buffer returned as "owned" by any function above. */
void nd_buffer_free(uint8_t *buf);

#ifdef __cplusplus
}
#endif

#endif /* NEXDESK_CODEC_H */
