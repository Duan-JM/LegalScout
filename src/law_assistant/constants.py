from law_assistant.plugins import csrc, shixin_csrc, sse_disclosure, szse_disclosure

AVALIABLE_SOURCES_FUNCS = {
    "csrc": csrc.api_v1,
    "shixin_csrc": shixin_csrc.api_v1,
    "sse_disclosure": sse_disclosure.api_v1,
    "szse_disclosure": szse_disclosure.api_v1,
}
