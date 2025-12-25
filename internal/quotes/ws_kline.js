import ws from "k6/ws";
import { Counter, Trend } from "k6/metrics";

const msgCount = new Counter("ws_msg_count");
const closeCount = new Counter("ws_close_count");
const errorCount = new Counter("ws_error_count");

const connectToOpen = new Trend("ws_connect_to_open_ms");
const openToFirst = new Trend("ws_open_to_first_ms");
const openToClose = new Trend("ws_open_to_close_ms");

export const options = {
    scenarios: {
        steady: {
            executor: "constant-vus",
            vus: Number(__ENV.VUS || 1000),
            duration: __ENV.DURATION || "2m",
        },
    },
};

export default function () {
    const url = __ENV.WS_URL || "ws://127.0.0.1:8080/ws";
    const topic = __ENV.TOPIC || "kline:1s:BTC-USD";

    const t0 = Date.now();
    let tOpen = 0;
    let gotFirst = false;

    ws.connect(url, {}, function (socket) {
        socket.on("open", function () {
            tOpen = Date.now();
            connectToOpen.add(tOpen - t0, { topic });

            socket.send(JSON.stringify({ type: "sub", topics: [topic] }));
        });

        socket.on("message", function () {
            msgCount.add(1, { topic });
            if (!gotFirst && tOpen > 0) {
                gotFirst = true;
                openToFirst.add(Date.now() - tOpen, { topic });
            }
        });

        socket.on("error", function (e) {
            // e 里通常只有 message（不同版本可能略有差异）
            errorCount.add(1, { topic });
            // console.log(`ws error topic=${topic}: ${e}`);
        });

        socket.on("close", function () {
            // 老 API 这里拿不到 code/reason，只能知道“发生了 close”
            closeCount.add(1, { topic });
            if (tOpen > 0) {
                openToClose.add(Date.now() - tOpen, { topic });
            }
            // console.log(`ws closed topic=${topic}`);
        });

        // 不主动关，让它尽量保持；到 duration 结束 k6 会中断 VU
        socket.setTimeout(() => {}, 24 * 60 * 60 * 1000);
    });
}
