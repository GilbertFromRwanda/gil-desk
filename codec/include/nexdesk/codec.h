#ifndef NEXDESK_CODEC_H
#define NEXDESK_CODEC_H

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

#ifdef __cplusplus
}
#endif

#endif /* NEXDESK_CODEC_H */
