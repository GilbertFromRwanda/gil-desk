#include "nexdesk/codec.h"

#include <cstdlib>

unsigned int nd_codec_abi_version(void) {
    /* Bumped from 1: adding the encoder/decoder ABI (planner Section 21 —
     * "ABI version check", "bump on any breaking change to the boundary"). */
    return 2;
}

void nd_buffer_free(uint8_t *buf) {
    std::free(buf);
}
