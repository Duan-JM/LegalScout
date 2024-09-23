from law_assistant.plugins import csrc, sse_disclosure, szse_disclosure, shixin_csrc

AVALIABLE_SOURCES_FUNCS = {
    "csrc": csrc.api_v1,
    "shixin_csrc": shixin_csrc.api_v1,
    "sse_disclosure": sse_disclosure.api_v1,
    "szse_disclosure": szse_disclosure.api_v1,
}
