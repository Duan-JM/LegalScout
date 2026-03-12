from law_assistant.plugins.csrc import CSRCPlugin
from law_assistant.plugins.shixin_csrc import ShixinCSRCPlugin
from law_assistant.plugins.sse_disclosure import SSEDisclosurePlugin
from law_assistant.plugins.szse_disclosure import SZSEDisclosurePlugin

AVAILABLE_SOURCES_FUNCS = {
    "csrc": CSRCPlugin().api_v1,
    "shixin_csrc": ShixinCSRCPlugin().api_v1,
    "sse_disclosure": SSEDisclosurePlugin().api_v1,
    "szse_disclosure": SZSEDisclosurePlugin().api_v1,
}
