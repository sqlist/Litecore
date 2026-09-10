# frontend-live · LiteCore 实验台

连接真实后端的网页前端（Vite + React + TypeScript）。这里的每一次注册、注销、在线列表和健康状态都来自运行中的 LiteCore 后端。

## 运行

```bash
npm install
npm run dev   # http://localhost:5173
```

前提：`amf`、`smf`、`upf` 和 `gateway`（HTTP 网关，:8080）已经启动。可用 docker 一键起后端：

```bash
# 在仓库根目录
docker compose up -d --build
go run ./gateway   # 或 make gateway
```

## 两种模式

- **实时连接**：网关可达时自动进入。页面每 1.5 秒同步在线终端，每 3 秒做一次健康检查。
- **演示模式**：网关连不上时自动降级，顶部出现提示横幅，注册退化为固定信道值的排练数据；页面每 5 秒重试一次网关，后端一起来就自动切回实时。

## 接口

全部经由 `gateway`（http://localhost:8080，CORS 允许本机 5173）：

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/api/register` | 注册一台 UE，网关侧按 UE 客户端同款策略重试 |
| POST | `/api/deregister` | 注销一台 UE |
| GET | `/api/ues` | 在线终端列表（含 UPF 转发包计数） |
| GET | `/api/health` | 网关 + AMF/SMF/UPF 健康状态 |

## 构建

```bash
npm run build   # 类型检查 + 产物输出到 dist/
```
