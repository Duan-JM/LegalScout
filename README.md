# Intro

帮助律师做一些信息收集的小脚本，来减轻律师大大们重复劳动的工具

**🚀 最新更新**：已迁移到 Playwright + Browserless 容器化方案，解决浏览器进程卡死宿主机的问题！

# Usages

## Installation

### 1. 安装 Python 依赖

```bash
poetry install
```

### 2. 启动 Browserless 容器

**方式一：使用 Docker Compose（推荐）**
```bash
docker-compose up -d
```

**方式二：直接使用 Docker**
```bash
docker run -d -p 3000:3000 \
  --name browserless \
  --restart unless-stopped \
  --memory=2g \
  -e "MAX_CONCURRENT_SESSIONS=10" \
  browserless/chrome:latest
```

### 3. 验证 Browserless 是否运行

```bash
curl http://localhost:3000/health
# 或访问浏览器：http://localhost:3000
```

## Basic Usage

```bash
python3 -m law_assistant.fetch_evidence \
  --input_file ./names.txt \
  --output_dir `pwd`/ot \
  --sources sse_disclosure,csrc,szse_disclosure,shixin_csrc \
  --process_num 5
```

**注意**：`process_num` 不要超过 Browserless 的 `MAX_CONCURRENT_SESSIONS` 配置

# Plugins

## Plugins

- [x] sse_disclosure: [上交所信息披露](http://www.sse.com.cn/home/search/)
- [x] szse_disclosure: [深交所信息披露](http://www.szse.cn/disclosure/supervision/measure/measure/index.html)
- [x] csrc: [政府信息公开](http://www.csrc.gov.cn/csrc/c100033/zfxxgk_zdgk.shtml#tab=gkzn)

## Multiple Input Plugins

- [x] shixin_csrc: [证券期货市场失信记录](https://neris.csrc.gov.cn/shixinchaxun/)
  - [x] name
  - [ ] id_card
  - [x] Need Verification Code
- [ ] REMAIN TODO: [法院失信被执行人](http://zxgk.court.gov.cn/shixin/)
  - [ ] name
  - [ ] id_card
  - [ ] Need Verification Code
  - [ ] Need Slide Verified
- [ ] REMAIN TODO: [专利和集成电路布图设计业务办理统一身份认证平台](https://tysf.cponline.cnipa.gov.cn/am/#/user/login)
  - Need Extra Login

# Example Usage

## Example Commands

```bash
# 单个数据源
python3 -m law_assistant.fetch_evidence \
  --input_file ./names.txt \
  --output_dir `pwd`/ot \
  --sources csrc \
  --process_num 5

# 多个数据源
python3 -m law_assistant.fetch_evidence \
  --input_file ./names.txt \
  --output_dir `pwd`/ot \
  --sources sse_disclosure,csrc,szse_disclosure,shixin_csrc \
  --process_num 5
```

## 环境变量配置
```bash
# 自定义 Browserless URL（默认：http://localhost:3000）
export BROWSERLESS_URL=http://192.168.1.100:3000

# 运行爬虫
python3 -m law_assistant.fetch_evidence --input_file ./names.txt --output_dir ./output --sources csrc --process_num 5 --debug 1
```