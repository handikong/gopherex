# wsdemo_steps (gnet-inspired)

5 separate minimal demos (run each independently).

## Run
```bash
cd <demo-folder>
go mod tidy
go run .
```

## Client
```bash
npm i -g wscat
wscat -c ws://127.0.0.1:8080/ws
> {"type":"sub","topics":["kline:1s:BTC-USD"]}
```

Folders:
- 01_sharded_hub
- 02_topic_id
- 03_preencode_payload
- 04_batch_and_pool
- 05_slow_client_backpressure
