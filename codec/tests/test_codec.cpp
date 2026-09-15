#include "nexdesk/codec.h"
#include <cassert>
#include <cstdio>

int main() {
    assert(nd_codec_abi_version() == 1);
    std::printf("codec_smoke_test: ok\n");
    return 0;
}
