#include "nexdesk/codec.h"

extern "C" {
#include <libavcodec/avcodec.h>
#include <libavutil/frame.h>
#include <libavutil/pixfmt.h>
}

#include <cstdlib>
#include <cstring>
#include <new>

struct nd_decoder {
    AVCodecContext *ctx;
    AVPacket *packet;
    AVFrame *frame;
};

nd_decoder_t *nd_decoder_create(void) {
    const AVCodec *codec = avcodec_find_decoder(AV_CODEC_ID_H264);
    if (!codec) {
        return nullptr;
    }

    AVCodecContext *ctx = avcodec_alloc_context3(codec);
    if (!ctx) {
        return nullptr;
    }
    if (avcodec_open2(ctx, codec, nullptr) < 0) {
        avcodec_free_context(&ctx);
        return nullptr;
    }

    AVPacket *packet = av_packet_alloc();
    AVFrame *frame = av_frame_alloc();
    if (!packet || !frame) {
        av_packet_free(&packet);
        av_frame_free(&frame);
        avcodec_free_context(&ctx);
        return nullptr;
    }

    auto *dec = new (std::nothrow) nd_decoder();
    if (!dec) {
        av_packet_free(&packet);
        av_frame_free(&frame);
        avcodec_free_context(&ctx);
        return nullptr;
    }
    dec->ctx = ctx;
    dec->packet = packet;
    dec->frame = frame;
    return dec;
}

nd_codec_status nd_decoder_decode(
    nd_decoder_t *decoder,
    const uint8_t *data, size_t len,
    uint8_t **out_data, size_t *out_len,
    int *out_width, int *out_height) {
    if (!decoder || !data || !out_data || !out_len || !out_width || !out_height) {
        return ND_CODEC_ERR_INVALID_ARG;
    }
    *out_data = nullptr;
    *out_len = 0;
    *out_width = 0;
    *out_height = 0;

    if (av_new_packet(decoder->packet, static_cast<int>(len)) < 0) {
        return ND_CODEC_ERR_NO_MEMORY;
    }
    std::memcpy(decoder->packet->data, data, len);

    int send_ret = avcodec_send_packet(decoder->ctx, decoder->packet);
    av_packet_unref(decoder->packet);
    if (send_ret < 0 && send_ret != AVERROR(EAGAIN)) {
        return ND_CODEC_ERR_DECODE;
    }

    int recv_ret = avcodec_receive_frame(decoder->ctx, decoder->frame);
    if (recv_ret == AVERROR(EAGAIN) || recv_ret == AVERROR_EOF) {
        return ND_CODEC_OK; /* needs more input; not an error */
    }
    if (recv_ret < 0) {
        return ND_CODEC_ERR_DECODE;
    }

    AVFrame *frame = decoder->frame;
    if (frame->format != AV_PIX_FMT_YUV420P) {
        av_frame_unref(frame);
        return ND_CODEC_ERR_UNSUPPORTED;
    }

    const int width = frame->width;
    const int height = frame->height;
    const size_t total = static_cast<size_t>(width) * height * 3 / 2;
    auto *buf = static_cast<uint8_t *>(std::malloc(total));
    if (!buf) {
        av_frame_unref(frame);
        return ND_CODEC_ERR_NO_MEMORY;
    }

    uint8_t *dst = buf;
    for (int row = 0; row < height; row++) {
        std::memcpy(dst, frame->data[0] + static_cast<ptrdiff_t>(row) * frame->linesize[0], width);
        dst += width;
    }
    for (int row = 0; row < height / 2; row++) {
        std::memcpy(dst, frame->data[1] + static_cast<ptrdiff_t>(row) * frame->linesize[1], width / 2);
        dst += width / 2;
    }
    for (int row = 0; row < height / 2; row++) {
        std::memcpy(dst, frame->data[2] + static_cast<ptrdiff_t>(row) * frame->linesize[2], width / 2);
        dst += width / 2;
    }

    av_frame_unref(frame);

    *out_data = buf;
    *out_len = total;
    *out_width = width;
    *out_height = height;
    return ND_CODEC_OK;
}

void nd_decoder_destroy(nd_decoder_t *decoder) {
    if (!decoder) {
        return;
    }
    av_packet_free(&decoder->packet);
    av_frame_free(&decoder->frame);
    avcodec_free_context(&decoder->ctx);
    delete decoder;
}
