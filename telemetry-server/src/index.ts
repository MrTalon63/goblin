import { Elysia } from "elysia";
import { initDb, saveReceiver, savePacket, getReceivers, getPackets, getPacketsCSV, getPacketsCSV_APID0, getPacketsCSV_APID1, getPacketsCSV_APID2, clearDb, checkpointDb, DB_PATH } from "./db";
import { join } from "path";

// Initialize SQLite database tables
initDb();

const app = new Elysia()
  // Serve the dashboard
  .get("/", () => {
    const dashboardPath = join(import.meta.dir, "dashboard.html");
    return new Response(Bun.file(dashboardPath), {
      headers: { "Content-Type": "text/html; charset=utf-8" }
    });
  })

  .get("/api/receivers", () => {
    return getReceivers();
  })

  .get("/api/telemetry", () => {
    return getPackets(1000);
  })

  .get("/api/telemetry/csv", ({ query, set }) => {
    const password = query.password;
    const expectedPassword = process.env.ADMIN_PASSWORD || "goblinadmin";
    if (password !== expectedPassword) {
      set.status = 401;
      return "Unauthorized: Incorrect password";
    }

    const csvContent = getPacketsCSV();
    return new Response(csvContent, {
      headers: {
        "Content-Type": "text/csv; charset=utf-8",
        "Content-Disposition": "attachment; filename=\"telemetry.csv\""
      }
    });
  })

  .get("/api/telemetry/csv/apid0", ({ query, set }) => {
    const password = query.password;
    const expectedPassword = process.env.ADMIN_PASSWORD || "goblinadmin";
    if (password !== expectedPassword) {
      set.status = 401;
      return "Unauthorized: Incorrect password";
    }

    const csvContent = getPacketsCSV_APID0();
    return new Response(csvContent, {
      headers: {
        "Content-Type": "text/csv; charset=utf-8",
        "Content-Disposition": "attachment; filename=\"telemetry_timesync_apid0.csv\""
      }
    });
  })

  .get("/api/telemetry/csv/apid1", ({ query, set }) => {
    const password = query.password;
    const expectedPassword = process.env.ADMIN_PASSWORD || "goblinadmin";
    if (password !== expectedPassword) {
      set.status = 401;
      return "Unauthorized: Incorrect password";
    }

    const csvContent = getPacketsCSV_APID1();
    return new Response(csvContent, {
      headers: {
        "Content-Type": "text/csv; charset=utf-8",
        "Content-Disposition": "attachment; filename=\"telemetry_core_apid1.csv\""
      }
    });
  })

  .get("/api/telemetry/csv/apid2", ({ query, set }) => {
    const password = query.password;
    const expectedPassword = process.env.ADMIN_PASSWORD || "goblinadmin";
    if (password !== expectedPassword) {
      set.status = 401;
      return "Unauthorized: Incorrect password";
    }

    const csvContent = getPacketsCSV_APID2();
    return new Response(csvContent, {
      headers: {
        "Content-Type": "text/csv; charset=utf-8",
        "Content-Disposition": "attachment; filename=\"telemetry_payload_apid2.csv\""
      }
    });
  })

  .get("/api/telemetry/db", ({ query, set }) => {
    const password = query.password;
    const expectedPassword = process.env.ADMIN_PASSWORD || "goblinadmin";
    if (password !== expectedPassword) {
      set.status = 401;
      return "Unauthorized: Incorrect password";
    }

    // Force a WAL checkpoint to flush all data from WAL file to main DB file before serving
    checkpointDb();

    const dbFile = Bun.file(DB_PATH);
    return new Response(dbFile, {
      headers: {
        "Content-Type": "application/x-sqlite3",
        "Content-Disposition": "attachment; filename=\"telemetry.db\""
      }
    });
  })

  .post("/api/telemetry/clear", ({ query, set }) => {
    const password = query.password;
    const expectedPassword = process.env.ADMIN_PASSWORD || "goblinadmin";
    if (password !== expectedPassword) {
      set.status = 401;
      return "Unauthorized: Incorrect password";
    }

    clearDb();
    return { status: "success", message: "Database cleared successfully" };
  })

  .ws("/ws", {
    open(ws) {
      console.log(`[WS] Connection opened: ${ws.id}`);
    },

    message(ws, rawMessage) {
      try {
        let payload: any;
        if (typeof rawMessage === "string") {
          payload = JSON.parse(rawMessage);
        } else if (rawMessage instanceof Buffer || rawMessage instanceof ArrayBuffer) {
          const decoder = new TextDecoder();
          payload = JSON.parse(decoder.decode(rawMessage));
        } else {
          payload = rawMessage;
        }

        if (!payload || typeof payload !== "object") return;

        const msgType = payload.type;

        if (msgType === "register") {
          const receiverId = payload.receiver_id;
          if (!receiverId) {
            console.error("[WS] Registration failed: Missing receiver_id");
            return;
          }

          ws.data = { ...((ws.data as object) || {}), receiverId };

          saveReceiver(receiverId, {
            id: receiverId,
            nickname: payload.nickname,
            lat: payload.lat,
            lon: payload.lon,
            alt: payload.alt,
            antenna: payload.antenna,
            radio: payload.radio
          });

          console.log(`[WS] Receiver registered: ${payload.nickname} (${receiverId})`);

          ws.send(JSON.stringify({ type: "ack", status: "registered", receiver_id: receiverId }));
        }
        else if (msgType === "telemetry") {
          const packetData = payload.packet;
          const apid = payload.apid;

          const receiverId = payload.receiver_id || (ws.data as any)?.receiverId || null;

          if (apid === undefined || !packetData) {
            console.error("[WS] Telemetry failed: Missing apid or packet data");
            return;
          }

          // If APID 2/CBOR metadata, we encode the full inner attributes as raw_json
          if (apid === 2 && typeof packetData === "object") {
            packetData.raw_json = JSON.stringify(packetData);
          }

          savePacket(receiverId, apid, packetData);
          console.log(`[WS] Telemetry saved: APID ${apid} from receiver ${receiverId || "unknown"}`);
        }
      } catch (err: any) {
        console.error("[WS] Error processing message:", err.message);
      }
    },

    close(ws) {
      console.log(`[WS] Connection closed: ${ws.id}`);
    }
  })

  .listen(3000);

console.log(
  `Goblin central telemetry server is running at ${app.server?.hostname}:${app.server?.port}`
);
