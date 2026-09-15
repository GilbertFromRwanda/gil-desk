#include "nexdesk/codec.h"

#include <cassert>
#include <cstdio>
#include <cstdlib>
#include <vector>

namespace {

// A synthetic I420 gradient frame — no real capture needed to prove the
// encoder/decoder round-trip actually works.
struct TestFrame {
    int width;
    int height;
    std::vector<uint8_t> y, u, v;

    static TestFrame make(int width, int height) {
        TestFrame f;
        f.width = width;
        f.height = height;
        f.y.resize(static_cast<size_t>(width) * height);
        f.u.resize(static_cast<size_t>(width) * height / 4);
        f.v.resize(static_cast<size_t>(width) * height / 4);
        for (int row = 0; row < height; row++) {
            for (int col = 0; col < width; col++) {
                f.y[static_cast<size_t>(row) * width + col] = static_cast<uint8_t>((row + col) & 0xFF);
            }
        }
        for (size_t i = 0; i < f.u.size(); i++) {
            f.u[i] = static_cast<uint8_t>(i & 0xFF);
            f.v[i] = static_cast<uint8_t>((i * 3) & 0xFF);
        }
        return f;
    }
};

void test_abi_version() {
    assert(nd_codec_abi_version() == 2);
}

void test_encoder_rejects_invalid_dimensions() {
    assert(nd_encoder_create(0, 0, 30, 1000) == nullptr);
    assert(nd_encoder_create(-1, 100, 30, 1000) == nullptr);
}

void test_encode_decode_round_trip_preserves_dimensions() {
    const int width = 64;
    const int height = 48; // multiple of 16, avoids x264 padding edge cases
    TestFrame frame = TestFrame::make(width, height);

    nd_encoder_t *encoder = nd_encoder_create(width, height, 30, 500);
    assert(encoder != nullptr);

    nd_decoder_t *decoder = nd_decoder_create();
    assert(decoder != nullptr);

    bool decoded_a_frame = false;

    // Feed several frames: x264 with zerolatency should emit NALs
    // promptly, but a decoder may still need >1 input packet before it
    // has enough state (SPS/PPS + first slice) to emit a picture.
    for (int i = 0; i < 5 && !decoded_a_frame; i++) {
        uint8_t *encoded = nullptr;
        size_t encoded_len = 0;
        nd_codec_status enc_status = nd_encoder_encode(
            encoder,
            frame.y.data(), width,
            frame.u.data(), width / 2,
            frame.v.data(), width / 2,
            &encoded, &encoded_len);
        assert(enc_status == ND_CODEC_OK);

        if (encoded_len == 0) {
            continue; // buffered internally; feed another frame
        }

        uint8_t *decoded = nullptr;
        size_t decoded_len = 0;
        int out_width = 0, out_height = 0;
        nd_codec_status dec_status = nd_decoder_decode(
            decoder, encoded, encoded_len, &decoded, &decoded_len, &out_width, &out_height);
        assert(dec_status == ND_CODEC_OK);
        nd_buffer_free(encoded);

        if (decoded_len > 0) {
            assert(out_width == width);
            assert(out_height == height);
            assert(decoded_len == static_cast<size_t>(width) * height * 3 / 2);
            nd_buffer_free(decoded);
            decoded_a_frame = true;
        }
    }

    assert(decoded_a_frame && "decoder never produced a frame within 5 input frames");

    nd_encoder_destroy(encoder);
    nd_decoder_destroy(decoder);
}

void test_decoder_rejects_garbage_without_crashing() {
    nd_decoder_t *decoder = nd_decoder_create();
    assert(decoder != nullptr);

    std::vector<uint8_t> garbage = {0x00, 0x01, 0x02, 0xFF, 0xAB, 0xCD};
    uint8_t *decoded = nullptr;
    size_t decoded_len = 0;
    int width = 0, height = 0;

    // Garbage input must not crash; either a clean error or "no frame yet"
    // are both acceptable, a crash is not.
    nd_decoder_decode(decoder, garbage.data(), garbage.size(), &decoded, &decoded_len, &width, &height);
    if (decoded) {
        nd_buffer_free(decoded);
    }

    nd_decoder_destroy(decoder);
}

} // namespace

int main() {
    test_abi_version();
    test_encoder_rejects_invalid_dimensions();
    test_encode_decode_round_trip_preserves_dimensions();
    test_decoder_rejects_garbage_without_crashing();
    std::printf("codec_test: ok (encoder/decoder round-trip verified)\n");
    return 0;
}
