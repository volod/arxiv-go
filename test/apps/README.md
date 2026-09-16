# External Test Applications

Put standalone executable helpers for external-process tests here when such a test needs one.
Keep them out of production packages and out of release builds. The current subprocess tests
reuse their own test binary, so there is no helper application here yet.
