# 实验方案

## 1. 低延迟实验

保持相同机器和相同Docker资源配置，分别运行：

```bash
go run ./ue -action benchmark -count 100 -concurrency 1 -scenario stable -run-id c1-r1 -output results/c1-r1.csv
go run ./ue -action benchmark -count 500 -concurrency 10 -scenario stable -run-id c10-r1 -output results/c10-r1.csv
go run ./ue -action benchmark -count 1000 -concurrency 50 -scenario stable -run-id c50-r1 -output results/c50-r1.csv
go run ./ue -action benchmark -count 2000 -concurrency 100 -scenario stable -run-id c100-r1 -output results/c100-r1.csv
```

每次重复都使用不同`run-id`，例如把`r1`依次改为`r2`、`r3`，避免命中上一轮的幂等快速路径。benchmark会在记录结果后自动注销本轮成功注册的UE；必须确认清理失败数为0。为了得到严格独立的重复实验，或清理有失败时，应在下一轮前重启整个AMF/SMF/UPF栈。

本地启动的SMF默认IP池只有254个地址；Docker和Kubernetes示例配置为2048。运行1000/2000 UE实验前必须调大`IP_POOL_SIZE`，或者使用已经配置为2048的Docker/Kubernetes栈。不能在池子不足时把“IP池耗尽”混进性能结果。

记录成功请求的平均延迟、P95、P99，以及总请求吞吐量和成功率。延迟分位数不包含信道拒绝、IP池耗尽或RPC失败。每组至少重复三次，报告中使用均值，并说明机器型号、CPU、内存、Go版本、IP池大小和运行方式。

不要把结果写成“达到5G/6G空口低延迟指标”，应表述为“LiteCore软件控制面端到端注册延迟”。

## 2. 信道感知实验

对stable、edge、degrading和mixed场景各运行1000个UE，固定`-seed 42`并为每次运行设置不同`run-id`。比较接入成功率，解释信号功率/SINR阈值如何影响结果。按当前70% stable、20% edge、10% degrading的分布和两个门限计算，mixed理论成功率约为80.1%；有限样本会围绕该值波动。

## 3. 可靠性实验

### 下游不可用

停止SMF后发起注册，预期AMF返回Unavailable而非崩溃；恢复SMF后再次注册应成功。

### 幂等性

同一个UE连续注册两次，预期session_id和ue_ip一致；并发注册时，一个请求可能先收到`Unavailable`，随后由客户端自动重试并得到同一资源。连续注销两次均成功，但注销请求必须携带正确的`amf_ue_id`。

### IP回收

将`IP_POOL_SIZE`设为1：UE-A注册后UE-B失败；注销UE-A后UE-B成功并获得回收的IP。

### Kubernetes恢复

持续发送请求时删除一个Pod，记录Kubernetes重建Pod所需时间。由于当前是内存状态，重建后原状态丢失必须如实记录；该实验验证的是进程恢复，不是状态无损高可用。

故障实验结束后重启整个栈，不能只重启单个服务后继续采集正式性能数据。AMF、SMF、UPF都保存内存状态，单服务重启会留下跨服务失配记录。

### 不确定响应

UPF可能已处理`CreateRule`，但成功回复在返回SMF途中丢失。当前没有持久化事务或后台孤儿清扫；重试会检测旧规则与新参数是否冲突，但不能保证自动清理不确定规则。该场景作为已知限制记录，不宣称“重试一定自愈”。

## 4. 建议图表

- 并发数—平均/P95/P99延迟折线图
- 并发数—吞吐量折线图
- 四种信道场景—接入成功率柱状图
- 故障发生前后—成功率时间序列图
