import { Database } from "bun:sqlite";
import { mkdirSync } from "fs";
import { join } from "path";

export const DB_PATH = join(process.cwd(), "data", "telemetry.db");

// Ensure data directory exists
mkdirSync(join(process.cwd(), "data"), { recursive: true });

const db = new Database(DB_PATH);

db.run("PRAGMA foreign_keys = ON;");
db.run("PRAGMA journal_mode = WAL;");
db.run("PRAGMA synchronous = NORMAL;");

export function initDb() {
  // Create receivers table
  db.run(`
    CREATE TABLE IF NOT EXISTS receivers (
      id TEXT PRIMARY KEY,
      nickname TEXT NOT NULL,
      lat REAL,
      lon REAL,
      alt REAL,
      antenna TEXT,
      radio TEXT,
      last_seen TEXT NOT NULL
    );
  `);

  // Create packets table
  db.run(`
    CREATE TABLE IF NOT EXISTS packets (
      id TEXT PRIMARY KEY,
      apid INTEGER NOT NULL,
      receiver_id TEXT,
      received_at TEXT NOT NULL,
      callsign TEXT NOT NULL,
      computed_time TEXT,
      time_offset INTEGER,
      latitude REAL,
      longitude REAL,
      altitude REAL,
      gps_sats INTEGER,
      gps_lock TEXT,
      batt_voltage REAL,
      temp_internal REAL,
      temp_external REAL,
      raw_json TEXT,
      FOREIGN KEY(receiver_id) REFERENCES receivers(id) ON DELETE SET NULL
    );
  `);

  // Indexes
  db.run("CREATE INDEX IF NOT EXISTS idx_packets_received_at ON packets(received_at);");
  db.run("CREATE INDEX IF NOT EXISTS idx_packets_receiver_id ON packets(receiver_id);");
}

export interface ReceiverInfo {
  id: string;
  nickname: string;
  lat: number | null;
  lon: number | null;
  alt: number | null;
  antenna: string | null;
  radio: string | null;
}

export function saveReceiver(id: string, info: ReceiverInfo) {
  const now = new Date().toISOString();

  // Upsert receiver
  const stmt = db.prepare(`
    INSERT INTO receivers (id, nickname, lat, lon, alt, antenna, radio, last_seen)
    VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8)
    ON CONFLICT(id) DO UPDATE SET
      nickname = excluded.nickname,
      lat = excluded.lat,
      lon = excluded.lon,
      alt = excluded.alt,
      antenna = excluded.antenna,
      radio = excluded.radio,
      last_seen = excluded.last_seen;
  `);

  stmt.run(
    id,
    info.nickname || "Anonymous Receiver",
    info.lat ?? null,
    info.lon ?? null,
    info.alt ?? null,
    info.antenna ?? null,
    info.radio ?? null,
    now
  );
}

export interface PacketData {
  callsign: string;
  computed_time?: string;
  time_offset_10ms?: number;
  latitude?: number;
  longitude?: number;
  altitude_m?: number;
  gps_sats?: number;
  gps_lock?: string;
  batt_voltage?: number;
  temp_internal?: number;
  temp_external?: number;
  raw_json?: string;
}

export function savePacket(receiverId: string | null, apid: number, data: PacketData) {
  const packetId = crypto.randomUUID();
  const now = new Date().toISOString();

  // If receiver exists, update its last_seen timestamp
  if (receiverId) {
    db.run(
      "UPDATE receivers SET last_seen = ?1 WHERE id = ?2;",
      now,
      receiverId
    );
  }

  const stmt = db.prepare(`
    INSERT INTO packets (
      id, apid, receiver_id, received_at, callsign, computed_time, time_offset,
      latitude, longitude, altitude, gps_sats, gps_lock, batt_voltage,
      temp_internal, temp_external, raw_json
    ) VALUES (
      ?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10, ?11, ?12, ?13, ?14, ?15, ?16
    );
  `);

  stmt.run(
    packetId,
    apid,
    receiverId,
    now,
    data.callsign || "UNKNOWN",
    data.computed_time ?? null,
    data.time_offset_10ms ?? null,
    data.latitude ?? null,
    data.longitude ?? null,
    data.altitude_m ?? null,
    data.gps_sats ?? null,
    data.gps_lock ?? null,
    data.batt_voltage ?? null,
    data.temp_internal ?? null,
    data.temp_external ?? null,
    data.raw_json ?? null
  );
}

export function getReceivers() {
  const query = db.prepare(`
    SELECT 
      r.id, r.nickname, r.lat, r.lon, r.alt, r.antenna, r.radio, r.last_seen,
      COUNT(p.id) as packet_count
    FROM receivers r
    LEFT JOIN packets p ON r.id = p.receiver_id
    GROUP BY r.id
    ORDER BY packet_count DESC;
  `);

  return query.all() as Array<{
    id: string;
    nickname: string;
    lat: number | null;
    lon: number | null;
    alt: number | null;
    antenna: string | null;
    radio: string | null;
    last_seen: string;
    packet_count: number;
  }>;
}

export function getPackets(limit = 1000) {
  const query = db.prepare(`
    SELECT * FROM packets 
    ORDER BY received_at DESC 
    LIMIT ?1;
  `);
  return query.all(limit);
}

export function getPacketsCSV(): string {
  const query = db.prepare(`
    SELECT 
      p.id, p.apid, p.receiver_id, r.nickname as receiver_nickname, 
      p.received_at, p.callsign, p.computed_time, p.time_offset,
      p.latitude, p.longitude, p.altitude, p.gps_sats, p.gps_lock, 
      p.batt_voltage, p.temp_internal, p.temp_external, p.raw_json
    FROM packets p
    LEFT JOIN receivers r ON p.receiver_id = r.id
    ORDER BY p.received_at ASC;
  `);

  const rows = query.all() as Array<any>;
  const headers = [
    "id", "apid", "receiver_id", "receiver_nickname", "received_at", "callsign",
    "computed_time", "time_offset", "latitude", "longitude", "altitude",
    "gps_sats", "gps_lock", "batt_voltage", "temp_internal", "temp_external", "raw_json"
  ];

  const escapeCSV = (val: any) => {
    if (val === null || val === undefined) return "";
    const str = String(val);
    if (str.includes(",") || str.includes('"') || str.includes("\n") || str.includes("\r")) {
      return `"${str.replace(/"/g, '""')}"`;
    }
    return str;
  };

  const csvRows = [headers.join(",")];
  for (const row of rows) {
    const values = headers.map(h => escapeCSV(row[h]));
    csvRows.push(values.join(","));
  }

  return csvRows.join("\n");
}

export function getPacketsCSV_APID0(): string {
  const query = db.prepare(`
    SELECT 
      p.id, p.receiver_id, r.nickname as receiver_nickname, 
      p.received_at, p.computed_time, p.time_offset, p.raw_json
    FROM packets p
    LEFT JOIN receivers r ON p.receiver_id = r.id
    WHERE p.apid = 0
    ORDER BY p.received_at ASC;
  `);

  const rows = query.all() as Array<any>;
  const headers = [
    "id", "receiver_id", "receiver_nickname", "received_at",
    "computed_time", "time_offset", "raw_json"
  ];

  const escapeCSV = (val: any) => {
    if (val === null || val === undefined) return "";
    const str = String(val);
    if (str.includes(",") || str.includes('"') || str.includes("\n") || str.includes("\r")) {
      return `"${str.replace(/"/g, '""')}"`;
    }
    return str;
  };

  const csvRows = [headers.join(",")];
  for (const row of rows) {
    const values = headers.map(h => escapeCSV(row[h]));
    csvRows.push(values.join(","));
  }

  return csvRows.join("\n");
}

export function getPacketsCSV_APID1(): string {
  const query = db.prepare(`
    SELECT 
      p.id, p.receiver_id, r.nickname as receiver_nickname, 
      p.received_at, p.callsign, p.computed_time, p.time_offset,
      p.latitude, p.longitude, p.altitude, p.gps_sats, p.gps_lock,
      p.batt_voltage, p.temp_internal, p.temp_external, p.raw_json
    FROM packets p
    LEFT JOIN receivers r ON p.receiver_id = r.id
    WHERE p.apid = 1
    ORDER BY p.received_at ASC;
  `);

  const rows = query.all() as Array<any>;
  const headers = [
    "id", "receiver_id", "receiver_nickname", "received_at", "callsign",
    "computed_time", "time_offset", "latitude", "longitude", "altitude",
    "gps_sats", "gps_lock", "batt_voltage", "temp_internal", "temp_external", "raw_json"
  ];

  const escapeCSV = (val: any) => {
    if (val === null || val === undefined) return "";
    const str = String(val);
    if (str.includes(",") || str.includes('"') || str.includes("\n") || str.includes("\r")) {
      return `"${str.replace(/"/g, '""')}"`;
    }
    return str;
  };

  const csvRows = [headers.join(",")];
  for (const row of rows) {
    const values = headers.map(h => escapeCSV(row[h]));
    csvRows.push(values.join(","));
  }

  return csvRows.join("\n");
}

export function getPacketsCSV_APID2(): string {
  const query = db.prepare(`
    SELECT 
      p.id, p.receiver_id, r.nickname as receiver_nickname, 
      p.received_at, p.callsign, p.computed_time, p.time_offset, p.raw_json
    FROM packets p
    LEFT JOIN receivers r ON p.receiver_id = r.id
    WHERE p.apid = 2
    ORDER BY p.received_at ASC;
  `);

  const rows = query.all() as Array<any>;
  const headers = [
    "id", "receiver_id", "receiver_nickname", "received_at", "callsign",
    "computed_time", "time_offset", "raw_json"
  ];

  const escapeCSV = (val: any) => {
    if (val === null || val === undefined) return "";
    const str = String(val);
    if (str.includes(",") || str.includes('"') || str.includes("\n") || str.includes("\r")) {
      return `"${str.replace(/"/g, '""')}"`;
    }
    return str;
  };

  const csvRows = [headers.join(",")];
  for (const row of rows) {
    const values = headers.map(h => escapeCSV(row[h]));
    csvRows.push(values.join(","));
  }

  return csvRows.join("\n");
}

export function clearDb() {
  db.run("DELETE FROM packets;");
  db.run("DELETE FROM receivers;");
  db.run("VACUUM;");
}

export function checkpointDb() {
  db.run("PRAGMA wal_checkpoint(TRUNCATE);");
}
