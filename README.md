# Plugins
## Only Name Plugins
- [x] sse_disclosure: [上交所信息披露](http://www.sse.com.cn/home/search/)
- [x] szse_disclosure: [深交所信息披露](http://www.szse.cn/disclosure/supervision/measure/measure/index.html)
- [x] csrc: [政府信息公开](http://www.csrc.gov.cn/csrc/c100033/zfxxgk_zdgk.shtml#tab=gkzn)

## Multiple Input Plugins
- [ ] 证券期货市场失信记录: [](https://neris.csrc.gov.cn/shixinchaxun/)
    - name
    - id_card
    - Need Verification Code
- [ ] 法院失信被执行人: [](http://zxgk.court.gov.cn/shixin/)
    - name
    - id_card
    - Need Verification Code
    - Need Jump Website
    - Need Slide Verified
- [ ] 专利和集成电路布图设计业务办理统一身份认证平台: [](https://tysf.cponline.cnipa.gov.cn/am/#/user/login)
    - Need Extra Login


# Example Usage
## Example Commands
```bash
python3 fetch_evidence.py --input_file ./names.txt --output_dir `pwd`/ot --sources sse_disclosure,csrc,szse_disclosure
```
