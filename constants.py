from plugins import csrc, sse_disclosure, szse_disclosure

AVALIABLE_SOURCES_FUNCS = {
    'csrc': csrc.api_v1,
    'sse_disclosure': sse_disclosure.api_v1,
    'szse_disclosure': szse_disclosure.api_v1
}
