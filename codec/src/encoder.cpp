#include "nexdesk/codec.h"

#include <x264.h>

#include <cstdlib>
#include <cstring>
#include <new>

struct nd_encoder {
    x264_t *handle;
    x264_picture_t pic_in;
    int width;
    int height;
};

nd_encoder_t *nd_encoder_create(int width, int height, int fps, int bitrate_kbps) {
    if (width <= 0 || height <= 0 || fps <= 0 || bitrate_kbps <= 0) {
        return nullptr;
    }

    x264_param_t param;
    if (x264_param_default_preset(&param, "veryfast", "zerolatency") < 0) {
        return nullptr;
    }
    param.i_width = width;
    param.i_height = height;
    param.i_fps_num = fps;
    param.i_fps_den = 1;
    param.i_csp = X264_CSP_I420;
    param.rc.i_rc_method = X264_RC_ABR;
    param.rc.i_bitrate = bitrate_kbps;
    param.b_repeat_headers = 1; /* SPS/PPS on every keyframe, so any NAL
                                   stream we produce is independently
                                   decodable from a keyframe onward. */
    param.b_annexb = 1;

    if (x264_param_apply_profile(&param, "baseline") < 0) {
        return nullptr;
    }

    x264_t *handle = x264_encoder_open(&param);
    if (!handle) {
        return nullptr;
    }

    auto *enc = new (std::nothrow) nd_encoder();
    if (!enc) {
        x264_encoder_close(handle);
        return nullptr;
    }
    enc->handle = handle;
    enc->width = width;
    enc->height = height;

    if (x264_picture_alloc(&enc->pic_in, X264_CSP_I420, width, height) < 0) {
        x264_encoder_close(handle);
        delete enc;
        return nullptr;
    }

    return enc;
}

nd_codec_status nd_encoder_encode(
    nd_encoder_t *encoder,
    const uint8_t *y, int y_stride,
    const uint8_t *u, int u_stride,
    const uint8_t *v, int v_stride,
    uint8_t **out_data, size_t *out_len) {
    if (!encoder || !y || !u || !v || !out_data || !out_len) {
        return ND_CODEC_ERR_INVALID_ARG;
    }
    *out_data = nullptr;
    *out_len = 0;

    for (int row = 0; row < encoder->height; row++) {
        std::memcpy(
            encoder->pic_in.img.plane[0] + static_cast<ptrdiff_t>(row) * encoder->pic_in.img.i_stride[0],
            y + static_cast<ptrdiff_t>(row) * y_stride,
            encoder->width);
    }
    for (int row = 0; row < encoder->height / 2; row++) {
        std::memcpy(
            encoder->pic_in.img.plane[1] + static_cast<ptrdiff_t>(row) * encoder->pic_in.img.i_stride[1],
            u + static_cast<ptrdiff_t>(row) * u_stride,
            encoder->width / 2);
        std::memcpy(
            encoder->pic_in.img.plane[2] + static_cast<ptrdiff_t>(row) * encoder->pic_in.img.i_stride[2],
            v + static_cast<ptrdiff_t>(row) * v_stride,
            encoder->width / 2);
    }

    x264_nal_t *nals;
    int nal_count = 0;
    x264_picture_t pic_out;
    int frame_size = x264_encoder_encode(encoder->handle, &nals, &nal_count, &encoder->pic_in, &pic_out);
    if (frame_size < 0) {
        return ND_CODEC_ERR_ENCODE;
    }
    if (frame_size == 0 || nal_count == 0) {
        return ND_CODEC_OK; /* buffered internally; no output yet */
    }

    auto *buf = static_cast<uint8_t *>(std::malloc(static_cast<size_t>(frame_size)));
    if (!buf) {
        return ND_CODEC_ERR_NO_MEMORY;
    }
    size_t offset = 0;
    for (int i = 0; i < nal_count; i++) {
        std::memcpy(buf + offset, nals[i].p_payload, static_cast<size_t>(nals[i].i_payload));
        offset += static_cast<size_t>(nals[i].i_payload);
    }

    *out_data = buf;
    *out_len = offset;
    return ND_CODEC_OK;
}

void nd_encoder_destroy(nd_encoder_t *encoder) {
    if (!encoder) {
        return;
    }
    x264_picture_clean(&encoder->pic_in);
    x264_encoder_close(encoder->handle);
    delete encoder;
}
