# 队友交接说明

## 先做这五件事

1. 阅读根目录README和`docs/ARCHITECTURE.md`。
2. 执行`make test`和`make race`。
3. 执行`docker compose up -d --build`。
4. 运行一次稳定UE、恶化UE和100个UE的压测。
5. 对照日志画出注册与注销时序，确保每个人都能解释。

不要从历史版本中复制或恢复`amf/amf`、`smf/smf`、`upf/upf`、`ue/ue`等编译产物；请使用`go run`、`go build`或Docker运行当前源码。

## 答辩时可以说

- 项目参考5G AMF/SMF/UPF职责，使用gRPC构建轻量化教学和实验平台。
- 信道模型通过接口只产出信号强度和SINR；-110 dBm和0 dB门限由AMF在首次注册时判断，可以替换计算模型而不改接入控制。
- 通过幂等、超时、并发保护、失败回滚、健康检查提高软件可靠性。
- 使用多UE实验衡量控制面延迟、吞吐量和成功率。
- UE注册后的信道恶化不触发下线，因为持续信道测量和移动性管理不在本教学范围内。
- Kubernetes演示的是进程被重新拉起；状态会丢失，不能表述为状态无损高可用。
- UE每次RPC最多等待4秒；注册遇到`Unavailable`、`Aborted`或`DeadlineExceeded`最多重试2次，CSV记录实际重试数。
- 同一UE的并发注册在服务端只创建一套资源；重叠请求可能先收到`Unavailable`，客户端随后重试并走幂等返回。

## 答辩时不能说

- “完整实现3GPP标准”。
- “可以连接真实5G手机或基站”。
- “UPF正在转发真实数据包”。
- “Kubernetes多副本保证状态无损高可用”。
- “实验延迟就是5G空口时延”。
- “gRPC健康服务正在被Docker/Kubernetes探针使用”（当前探针是TCP端口检查）。
- “UPF回复丢失后重试一定能自愈”或“系统会自动清扫孤儿规则”。

## 推荐分工

- 负责人A：维护AMF/SMF/UPF代码和Proto接口。
- 负责人B：执行低延迟、信道和故障实验，保存原始CSV和机器环境。
- 负责人C：Docker/K8s演示、README、报告和答辩PPT。

所有人都必须能解释注册链路、注销链路、低延迟测量口径和项目边界。

## 交付检查表

- [ ] 所有测试和竞态检查通过
- [ ] Docker三服务healthy
- [ ] stable注册成功，degrading被拒绝
- [ ] 注销后IP可以回收
- [ ] benchmark清理失败数为0，CSV包含retry_count
- [ ] 至少四组并发实验，每组重复三次
- [ ] 至少一组故障恢复实验
- [ ] 报告包含机器环境和原始CSV
- [ ] PPT明确声明仿真平台边界

## 后续扩展优先级

1. 接入导师给出的信道模型数据。
2. 增加OpenTelemetry/Prometheus，获得更精细的分段延迟。
3. 引入Redis保存共享状态后再扩AMF/SMF副本。
4. 用UDP或TUN/TAP实现教学型真实数据转发。
