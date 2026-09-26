## Software Requirement Specification (SRS)
# AI QC Checker Platform
**Version:** 1.0 (Draft) · **สถาปัตยกรรม:** Event-Driven Microservices + Domain-Driven Design

---

## 1. บทนำ

### 1.1 วัตถุประสงค์
เอกสารนี้ระบุความต้องการของระบบ **AI QC Checker** — แพลตฟอร์มตรวจสอบคุณภาพชิ้นงานกราฟิก/เอกสารอัตโนมัติ โดยผู้ใช้กำหนด "ชุดกฎ" (Rule Set) เองได้ และระบบประมวลผลแบบ asynchronous pipeline ผ่าน event bus

### 1.2 ขอบเขต (Scope)
| อยู่ในขอบเขต | อยู่นอกขอบเขต (Phase 1) |
|---|---|
| จัดการ Project / Specimen (ชิ้นงาน) | การแก้ไขไฟล์อัตโนมัติ (auto-fix) |
| กำหนด Rule Set แบบ plug-in ต่อได้ | Video QC |
| ตรวจสะกด / สี / โลโก้ / ขนาดภาพ / ขนาดไฟล์ | Mobile native app |
| รายงานผล + Human Review | On-prem air-gapped deployment |
| Traceability ของโมเดล AI | Billing / Subscription |

### 1.3 Ubiquitous Language (สรุป)
| คำศัพท์ | ความหมาย |
|---|---|
| **Project** | พื้นที่ทำงานที่รวมชิ้นงาน + Rule Set + สมาชิก |
| **Specimen** | ชิ้นงาน 1 ชิ้นที่ถูกตรวจ (1 ไฟล์ + metadata) |
| **Rule** | เกณฑ์ตรวจ 1 ข้อ มี type + parameters |
| **Rule Set** | ชุดกฎที่มี version ผูกกับ Project |
| **Checker** | microservice ที่ execute rule หนึ่งประเภท |
| **Inspection Job** | คำสั่งตรวจ 1 ครั้งของ Specimen 1 ชิ้น |
| **Check Task** | หน่วยงานย่อย = 1 Rule × 1 Specimen |
| **Finding** | ข้อบกพร่องที่ตรวจพบ 1 รายการ |
| **Verdict** | ผลสรุป: Passed / Failed / Inconclusive |
| **Adjudication** | การที่มนุษย์ยืนยันหรือกลับผลของ AI |

---

## 2. ภาพรวมระบบ

### 2.1 Bounded Contexts → Microservices

| Bounded Context | Service | ภาษา | หน้าที่หลัก |
|---|---|---|---|
| Identity & Access | `iam-svc` | Go | Auth, Tenant, RBAC |
| Project Workspace | `project-svc` | Go | Project, Member, Specimen metadata |
| Quality Standard | `ruleset-svc` | Go | Rule Registry, Rule Set versioning |
| Specimen Intake | `intake-svc` | Go | Presigned URL, รับ S3 event, normalize |
| **Inspection (Core)** | `inspection-svc` | Go | Orchestrator, fan-out/fan-in, Verdict |
| Checker: Spelling | `checker-spelling` | Python | OCR + spell check |
| Checker: Color | `checker-color` | Python | OpenCV + ΔE CIEDE2000 |
| Checker: Logo | `checker-logo` | Python | Template match / embedding similarity |
| Checker: Dimension | `checker-dimension` | Go | ขนาด px, aspect ratio, DPI |
| Checker: FileSize | `checker-filesize` | Go | ขนาดไฟล์, format |
| Adjudication | `review-svc` | Go | คิวรีวิว, override |
| Reporting | `report-svc` | Go | Aggregate, export PDF/XLSX |
| Notification | `notify-svc` | Go | Email / LINE / Webhook |

> **หลักการ:** Checker ทุกตัวคือ *pluggable component* — เพิ่มกฎใหม่ = deploy service ใหม่ + register manifest โดย **ไม่แก้โค้ด `inspection-svc`** (Open/Closed Principle ระดับสถาปัตยกรรม)

### 2.2 Context Diagram (text)

```
React SPA ──HTTPS──> API Gateway ──> [iam | project | ruleset | inspection | review | report]
    │                                            │
    └──PUT presigned──> RustFS (S3 Compat) ──S3 Event Notification──> NATS JetStream
                             ▲                                            │
                             │                                            ▼
                        stream object                            inspection-svc (orchestrator)
                             │                                            │ fan-out
                             └──────────── checkers (Go/Python) <─────────┘
                                                  │ fan-in (CheckTaskCompleted)
                                                  ▼
                                          Verdict → Postgres + Event → notify/report
```

---

## 3. Functional Requirements

### 3.1 Project & Workspace Hierarchy

**โครงสร้าง:** `Tenant > Project > Specimen > Inspection Job > Finding`

| ID | ความต้องการ | Priority |
|---|---|---|
| FR-P01 | ผู้ใช้สร้าง / แก้ไข / archive **Project** ได้ แต่ละ Project มี `name`, `code`, `description`, `defaultRuleSetId` | Must |
| FR-P02 | Project เป็น **isolation boundary** — Specimen, Rule Set, รายงาน มองเห็นได้เฉพาะสมาชิกของ Project | Must |
| FR-P03 | เชิญสมาชิกเข้า Project พร้อมบทบาท: `Owner` / `QC Manager` / `Inspector` / `Viewer` | Must |
| FR-P04 | อัปโหลด **Specimen** เข้า Project ได้ทั้งแบบไฟล์เดี่ยวและ batch (multi-select / ZIP) | Must |
| FR-P05 | จัดกลุ่ม Specimen ด้วย `folder path` และ `tags` (เช่น `campaign-q1`, `banner`) | Should |
| FR-P06 | ดูประวัติการตรวจทุกครั้งของ Specimen หนึ่งชิ้น (re-run ได้ เก็บทุก revision) | Must |
| FR-P07 | Soft delete + Recycle bin 30 วัน | Should |

### 3.2 Rule Management (หัวใจของความยืดหยุ่น)

| ID | ความต้องการ | Priority |
|---|---|---|
| FR-R01 | ระบบมี **Rule Registry** เก็บ *Rule Type* ที่ติดตั้งอยู่ แต่ละตัวมี **Rule Manifest** | Must |
| FR-R02 | Rule Manifest ประกอบด้วย: `ruleType`, `version`, `displayName`, `paramSchema` (JSON Schema), `supportedMimeTypes`, `subject`, `defaultSeverity`, `timeoutSec` | Must |
| FR-R03 | Checker service ใหม่ **ประกาศตัวเอง (self-register)** ผ่าน NATS subject `qc.registry.announce` ตอน startup → UI เห็นกฎใหม่ทันทีโดยไม่ deploy frontend ใหม่ | Must |
| FR-R04 | Frontend **render ฟอร์มพารามิเตอร์อัตโนมัติ** จาก `paramSchema` (JSON-Schema-driven form) | Must |
| FR-R05 | ผู้ใช้ **เพิ่ม / ลบ / เปิด-ปิด / จัดลำดับ** Rule ใน Rule Set ได้ | Must |
| FR-R06 | แต่ละ Rule ใน Rule Set กำหนดได้: `enabled`, `severity` (Critical/Major/Minor), `params`, `blocking` (ถ้า fail ให้หยุด pipeline ทันทีหรือไม่) | Must |
| FR-R07 | Rule Set เป็น **immutable version** — แก้ไขแล้วสร้าง version ใหม่ (v1, v2…) Inspection Job อ้างอิง version ที่ใช้เสมอ | Must |
| FR-R08 | Clone Rule Set ข้าม Project และบันทึกเป็น **Template** ระดับ Tenant | Should |
| FR-R09 | **Dry-run / Preview**: ทดสอบ Rule Set กับ Specimen ตัวอย่างก่อนบังคับใช้จริง | Should |
| FR-R10 | Rule Set ต้องผ่านการ **Approve** โดย QC Manager ก่อนสถานะเป็น `Published` | Could |

#### Rule Manifest ตัวอย่าง
```json
{
  "ruleType": "COLOR_PALETTE_COMPLIANCE",
  "version": "1.2.0",
  "displayName": "ตรวจสีตาม Brand Palette",
  "engine": "python-opencv",
  "subject": "qc.check.color.v1",
  "supportedMimeTypes": ["image/png", "image/jpeg", "application/pdf"],
  "defaultSeverity": "MAJOR",
  "timeoutSec": 30,
  "paramSchema": {
    "type": "object",
    "required": ["palette", "deltaEThreshold"],
    "properties": {
      "palette": {
        "type": "array",
        "title": "สีที่อนุญาต (HEX)",
        "items": { "type": "string", "pattern": "^#[0-9A-Fa-f]{6}$" }
      },
      "deltaEThreshold": {
        "type": "number", "title": "ค่า ΔE สูงสุดที่ยอมรับ",
        "default": 3.0, "minimum": 0, "maximum": 20
      },
      "minAreaRatio": {
        "type": "number", "title": "สัดส่วนพื้นที่ขั้นต่ำที่นับเป็นสีหลัก",
        "default": 0.02
      }
    }
  }
}
```

### 3.3 Rule Types ที่ต้องมีใน Phase 1

| ID | Rule Type | Engine | Parameters หลัก | Finding ที่คืน |
|---|---|---|---|---|
| FR-RT01 | `SPELLING_CHECK` | Python (Tesseract/PaddleOCR + PyThaiNLP + LanguageTool) | `languages[]`, `customDictionary[]`, `ignorePatterns[]`, `minConfidence` | คำผิด, ตำแหน่ง bbox, คำที่แนะนำ |
| FR-RT02 | `COLOR_PALETTE_COMPLIANCE` | Python (OpenCV, skimage) | `palette[]`, `deltaEThreshold`, `minAreaRatio`, `colorSpace` | สีที่หลุด palette, ΔE, พื้นที่, ตำแหน่ง |
| FR-RT03 | `LOGO_PRESENCE` | Python (OpenCV template match + CLIP/ORB) | `logoAssetId`, `minSimilarity`, `allowedZones[]`, `minSizeRatio`, `clearSpaceRatio` | พบ/ไม่พบ, similarity, ตำแหน่ง, ละเมิด clear space |
| FR-RT04 | `IMAGE_DIMENSION` | Go (imaging) | `minWidth/minHeight`, `maxWidth/maxHeight`, `exactSize`, `aspectRatio`, `minDPI` | ขนาดจริง vs ที่กำหนด |
| FR-RT05 | `FILE_SIZE_LIMIT` | Go | `maxSizeMB`, `minSizeKB`, `allowedFormats[]` | ขนาดจริง (bytes), format |

> **FR-RT06 (Extensibility):** เพิ่ม Rule Type ใหม่ (เช่น `FONT_COMPLIANCE`, `NSFW_DETECTION`, `BARCODE_VALIDITY`, `TEXT_CONTRAST_WCAG`) ต้องทำได้โดย **ไม่แก้ service เดิมใดๆ** — เพียง implement Checker Contract แล้ว deploy

### 3.4 Upload & Intake

| ID | ความต้องการ |
|---|---|
| FR-U01 | Frontend **ไม่อัปโหลดผ่าน backend** — ขอ **presigned PUT URL** จาก `intake-svc` แล้ว upload ตรงเข้า RustFS |
| FR-U02 | รองรับ **multipart upload** สำหรับไฟล์ > 100 MB และ resume ได้ |
| FR-U03 | Object key pattern: `t/{tenantId}/p/{projectId}/s/{specimenId}/{revision}/original.{ext}` |
| FR-U04 | RustFS ส่ง **S3 Event Notification (`s3:ObjectCreated:*`)** เข้า NATS subject `qc.storage.object.created` |
| FR-U05 | `intake-svc` consume event → verify checksum (ETag) → สร้าง Specimen revision → extract metadata (dimension, DPI, mime, pages) → สร้าง thumbnail/preview → publish `SpecimenIngested` |
| FR-U06 | ปฏิเสธไฟล์ที่ MIME ไม่อยู่ใน allowlist หรือ magic-number ไม่ตรงนามสกุล |
| FR-U07 | Anti-virus scan (ClamAV) ก่อนเข้า pipeline; ติดเชื้อ → quarantine bucket |
| FR-U08 | Idempotency: object key + ETag เดิม จะไม่สร้าง Specimen ซ้ำ |

### 3.5 Inspection Pipeline

| ID | ความต้องการ |
|---|---|
| FR-I01 | เมื่อได้ `SpecimenIngested` → `inspection-svc` สร้าง **Inspection Job** พร้อม **snapshot ของ Rule Set** ที่ใช้ |
| FR-I02 | **Fan-out:** สร้าง Check Task ละ 1 Rule แล้ว publish ไปยัง subject ของ Checker นั้นๆ ขนานกัน |
| FR-I03 | Checker ที่ไม่รองรับ mimeType ของ Specimen จะถูกข้าม พร้อมบันทึก `SKIPPED` + เหตุผล |
| FR-I04 | **Fan-in:** รวบรวม `CheckTaskCompleted` จนครบทุก task → คำนวณ Verdict |
| FR-I05 | Verdict Policy: มี Finding `CRITICAL` ≥1 → `FAILED`; มี `MAJOR` เกิน threshold → `FAILED`; มี task `INCONCLUSIVE` หรือ confidence ต่ำกว่าเกณฑ์ → `NEEDS_REVIEW`; นอกนั้น → `PASSED` |
| FR-I06 | รองรับ `blocking` rule — fail แล้วยกเลิก task ที่เหลือทันทีเพื่อประหยัด compute |
| FR-I07 | **Timeout:** Job ที่ไม่ครบภายใน `jobTimeout` → `PARTIAL` + ระบุ task ที่ค้าง |
| FR-I08 | **Retry:** Check Task ล้มเหลวชั่วคราว retry ตาม exponential backoff (สูงสุด 3 ครั้ง) แล้วเข้า **DLQ** |
| FR-I09 | ทุก Check Result ต้องบันทึก **Model Attribution**: `checkerName`, `checkerVersion`, `modelRevision`, `durationMs` |
| FR-I10 | Re-run ตรวจซ้ำด้วย Rule Set version ใหม่ได้ โดยผลเดิมยังคงอยู่ (audit trail) |
| FR-I11 | Batch Inspection: สั่งตรวจทั้ง Project หรือทั้ง tag ได้ในคำสั่งเดียว |

### 3.6 Result, Review & Reporting

| ID | ความต้องการ |
|---|---|
| FR-V01 | หน้า Result แสดง preview ชิ้นงานพร้อม **overlay bounding box** ของทุก Finding คลิกดูรายละเอียดได้ |
| FR-V02 | Filter Finding ตาม rule type / severity / confidence |
| FR-V03 | Inspector **Adjudicate** ได้: `Confirm` / `Dismiss (false positive)` / `Override Verdict` พร้อมบังคับใส่เหตุผล |
| FR-V04 | การ override บันทึกผู้ทำ + เวลา + reason → เป็น **feedback dataset** สำหรับปรับโมเดล |
| FR-V05 | Real-time progress ของ Job บน UI ผ่าน WebSocket/SSE (bridge จาก NATS) |
| FR-V06 | Dashboard: Pass rate, Top defects (Pareto), เวลาตรวจเฉลี่ย, false-positive rate ต่อ rule |
| FR-V07 | Export รายงานเป็น PDF / XLSX / JSON |
| FR-V08 | Webhook / LINE Notify เมื่อ Job เสร็จ หรือพบ Critical finding |

---

## 4. Domain Model (Inspection Context)

```
InspectionJob  «Aggregate Root»
├── InspectionJobId          : VO
├── TenantId, ProjectId      : VO
├── SpecimenRef              : VO { specimenId, revision, objectKey, checksum, mimeType, sizeBytes }
├── RuleSetSnapshot          : VO { ruleSetId, version, rules[] }   ← immutable copy
├── status                   : QUEUED → DISPATCHED → ANALYZING → EVALUATED → ADJUDICATING → CLOSED | FAILED
├── CheckTask[]              : «Entity»
│   ├── taskId, ruleId, ruleType, state (PENDING|RUNNING|DONE|SKIPPED|FAILED)
│   ├── attribution : VO { checkerName, checkerVersion, modelRevision, durationMs }
│   └── Finding[]   : «Entity» { defectType, severity, confidence, region, message, evidenceRef }
├── Verdict                  : VO { outcome, decidedBy (AI|HUMAN), decidedAt, reasonCode }
└── domainEvents[]

Invariants:
 INV-1  ปิด Job (CLOSED) ไม่ได้ถ้ายังมี CheckTask ที่ไม่ใช่ terminal state
 INV-2  RuleSetSnapshot แก้ไขไม่ได้หลังสร้าง Job
 INV-3  Verdict ถูกตั้งได้เฉพาะตอน status = EVALUATED ขึ้นไป
 INV-4  Finding ทุกตัวต้อง trace กลับไปยัง ruleId ที่อยู่ใน snapshot
 INV-5  การ Override ต้องมี reason ความยาว ≥ 10 ตัวอักษร
```

**Repository:** `InspectionJobRepository` (1 aggregate = 1 transaction boundary)
**Domain Services:** `VerdictPolicyService`, `RuleDispatchPlanner`
**ACL:** `CheckerResultTranslator` แปลง output ดิบของโมเดล (tensor/label) → `Finding` ที่เป็นภาษาธุรกิจ

---

## 5. Event Catalog (NATS JetStream)

### 5.1 Subject Naming Convention
`qc.<context>.<aggregate>.<event>.<version>`

| Subject | Publisher | Consumer | Payload หลัก |
|---|---|---|---|
| `qc.storage.object.created` | RustFS | intake-svc | bucket, key, etag, size |
| `qc.intake.specimen.ingested.v1` | intake-svc | inspection-svc | specimenId, revision, metadata |
| `qc.inspection.job.created.v1` | inspection-svc | report, notify | jobId, ruleSetVersion |
| `qc.check.spelling.v1` | inspection-svc | checker-spelling | taskId, objectKey, params |
| `qc.check.color.v1` | inspection-svc | checker-color | taskId, objectKey, params |
| `qc.check.logo.v1` | inspection-svc | checker-logo | taskId, objectKey, params |
| `qc.check.dimension.v1` | inspection-svc | checker-dimension | taskId, objectKey, params |
| `qc.check.filesize.v1` | inspection-svc | checker-filesize | taskId, objectKey, params |
| `qc.inspection.task.completed.v1` | checkers | inspection-svc | taskId, findings[], attribution |
| `qc.inspection.task.failed.v1` | checkers | inspection-svc | taskId, errorCode, retryable |
| `qc.inspection.job.evaluated.v1` | inspection-svc | review, notify, report | jobId, verdict, summary |
| `qc.review.verdict.overridden.v1` | review-svc | inspection, report | jobId, newVerdict, reason |
| `qc.registry.announce` | ทุก checker | ruleset-svc | rule manifest |

### 5.2 JetStream Configuration

| Stream | Subjects | Retention | Replicas | Consumer Type |
|---|---|---|---|---|
| `QC_INTAKE` | `qc.storage.*`, `qc.intake.*` | WorkQueue, 7d | 3 | Durable Pull |
| `QC_CHECK` | `qc.check.*` | WorkQueue, 24h | 3 | Durable Pull + queue group (horizontal scale) |
| `QC_RESULT` | `qc.inspection.*`, `qc.review.*` | Limits, 30d | 3 | Durable Pull |
| `QC_DLQ` | `qc.dlq.>` | Limits, 90d | 3 | Manual |

- **AckPolicy:** Explicit · **AckWait:** 60s (checker ที่หนักใช้ `InProgress` heartbeat)
- **MaxDeliver:** 4 → เกินแล้วเข้า `QC_DLQ`
- **Deduplication:** `Nats-Msg-Id = taskId` (window 2 นาที) เพื่อ exactly-once effect
- **Backpressure:** `MaxAckPending` ต่อ consumer ปรับตามกำลัง GPU/CPU

### 5.3 Event Envelope (CloudEvents 1.0)
```json
{
  "specversion": "1.0",
  "id": "01J8X...",
  "source": "qc/inspection-svc",
  "type": "qc.inspection.task.completed.v1",
  "subject": "job/01J8X.../task/07",
  "time": "2025-01-15T09:12:33Z",
  "traceparent": "00-4bf92f...-00f067aa0ba902b7-01",
  "tenantid": "tnt_001",
  "datacontenttype": "application/json",
  "data": {
    "jobId": "01J8X...",
    "taskId": "07",
    "ruleType": "COLOR_PALETTE_COMPLIANCE",
    "state": "DONE",
    "findings": [
      {
        "defectType": "OFF_PALETTE_COLOR",
        "severity": "MAJOR",
        "confidence": 0.94,
        "region": { "kind": "bbox", "x": 120, "y": 340, "w": 88, "h": 44 },
        "message": "พบสี #E13A2B ต่างจาก brand #D0021B (ΔE = 6.8)",
        "evidenceRef": "t/tnt_001/.../evidence/task07-heatmap.png"
      }
    ],
    "attribution": {
      "checkerName": "checker-color",
      "checkerVersion": "1.2.0",
      "modelRevision": "n/a",
      "durationMs": 412
    }
  }
}
```

---

## 6. Checker Contract (สัญญาสำหรับเพิ่มกฎใหม่)

Checker ทุกตัวต้องทำ 5 อย่างนี้ ถึงจะ plug เข้าระบบได้:

1. **Announce** — publish Rule Manifest ไปที่ `qc.registry.announce` ตอน startup และทุก 60 วินาที (heartbeat)
2. **Subscribe** — ผูก durable pull consumer กับ subject ของตัวเองใน queue group เดียวกัน (scale ได้แนวราบ)
3. **Stream** — ดึง object จาก RustFS ด้วย presigned GET แบบ **streaming** (ห้ามโหลดทั้งไฟล์เข้า RAM สำหรับไฟล์ > 50 MB) รองรับ **Range request** เพื่ออ่านเฉพาะ header ตอนตรวจ metadata
4. **Emit** — publish `CheckTaskCompleted` หรือ `CheckTaskFailed` พร้อม `Nats-Msg-Id = taskId`
5. **Be Idempotent** — ได้รับ task เดิมซ้ำต้องให้ผลเหมือนเดิมและไม่สร้าง side effect ซ้ำ

**Interface (Python SDK):**
```python
class Checker(Protocol):
    manifest: RuleManifest

    async def execute(
        self,
        specimen: SpecimenStream,   # .reader(), .meta, .range(start, end)
        params: dict,               # ผ่านการ validate ด้วย paramSchema แล้ว
        ctx: CheckContext,          # trace_id, deadline, heartbeat()
    ) -> CheckResult:               # findings[] + attribution
        ...
```

---

## 7. Non-Functional Requirements

### 7.1 Performance
| ID | ความต้องการ |
|---|---|
| NFR-P01 | Intake latency (upload เสร็จ → Job created) ≤ 3 วินาที (p95) |
| NFR-P02 | Rule เบา (dimension, filesize) ≤ 500 ms ต่อ task (p95) |
| NFR-P03 | Rule หนัก (spelling OCR, logo) ≤ 15 วินาที ต่อภาพ 4K (p95) |
| NFR-P04 | Throughput ≥ 500 Specimen/นาที ที่ 20 checker replicas |
| NFR-P05 | API อ่านข้อมูล (GET) ≤ 300 ms (p95) |
| NFR-P06 | UI แสดง progress อัปเดตภายใน 1 วินาทีหลังเกิด event |

### 7.2 Scalability & Availability
| ID | ความต้องการ |
|---|---|
| NFR-S01 | ทุก service **stateless** → scale แนวราบด้วย HPA ตาม NATS consumer pending count |
| NFR-S02 | NATS cluster 3 nodes, JetStream R=3, ทนการล่มของ 1 node |
| NFR-S03 | Availability ≥ 99.5% (ช่วงเวลาทำการ) |
| NFR-S04 | การล่มของ Checker ตัวใดตัวหนึ่งต้อง **ไม่ทำให้ Job ทั้งระบบล้ม** — Job จบเป็น `PARTIAL` แทน |
| NFR-S05 | Graceful shutdown: drain consumer ให้ในงานที่ค้างเสร็จภายใน 30 วินาทีก่อนปิด |

### 7.3 Reliability
| ID | ความต้องการ |
|---|---|
| NFR-R01 | **At-least-once delivery** + idempotent consumer = ผลลัพธ์แบบ effectively-once |
| NFR-R02 | ใช้ **Transactional Outbox** ใน service ที่มี DB เพื่อกัน dual-write |
| NFR-R03 | ทุก consumer ต้องมี DLQ + หน้า UI สำหรับ replay งานใน DLQ |
| NFR-R04 | RPO ≤ 15 นาที, RTO ≤ 1 ชั่วโมง |
| NFR-R05 | Object storage versioning เปิดใช้งาน ป้องกันเขียนทับโดยไม่ตั้งใจ |

### 7.4 Security
| ID | ความต้องการ |
|---|---|
| NFR-SEC01 | OIDC / OAuth2 + JWT (access 15 นาที, refresh 7 วัน) |
| NFR-SEC02 | RBAC ระดับ Project + row-level isolation ด้วย `tenant_id` ทุก query |
| NFR-SEC03 | Presigned URL อายุ ≤ 15 นาที, scope เฉพาะ object key เดียว |
| NFR-SEC04 | NATS ใช้ mTLS + account/NKey แยกตาม service (least privilege ต่อ subject) |
| NFR-SEC05 | Encryption at rest (RustFS SSE) + in transit (TLS 1.3) |
| NFR-SEC06 | Audit log แบบ append-only สำหรับ: login, แก้ Rule Set, override verdict, ลบข้อมูล |
| NFR-SEC07 | Rate limit ต่อ tenant: 100 req/s (API), 1,000 upload/ชั่วโมง |

### 7.5 Observability
| ID | ความต้องการ |
|---|---|
| NFR-O01 | Structured logging (JSON) พร้อม `trace_id`, `job_id`, `tenant_id` ทุกบรรทัด |
| NFR-O02 | Distributed tracing ด้วย OpenTelemetry — trace context propagate ผ่าน NATS header |
| NFR-O03 | Metrics (Prometheus): `qc_job_duration_seconds`, `qc_task_failures_total{rule_type}`, `nats_consumer_pending`, `qc_false_positive_ratio` |
| NFR-O04 | Alert เมื่อ DLQ depth > 10, consumer lag > 1,000, error rate > 5% |
| NFR-O05 | Health endpoints: `/healthz` (liveness), `/readyz` (พร้อมเมื่อเชื่อม NATS + DB สำเร็จ) |

### 7.6 Maintainability
| ID | ความต้องการ |
|---|---|
| NFR-M01 | Event schema versioned + backward compatible; breaking change = subject version ใหม่ |
| NFR-M02 | Contract test ระหว่าง publisher/consumer ใน CI (schema registry) |
| NFR-M03 | โค้ดโครงสร้างแบบ Hexagonal: `domain/` ห้าม import `infrastructure/` (บังคับด้วย lint rule) |
| NFR-M04 | Test coverage: domain layer ≥ 85%, overall ≥ 70% |
| NFR-M05 | เพิ่ม Checker ใหม่ต้องเสร็จได้ภายใน ≤ 1 วันทำงาน โดยไม่แก้ service อื่น |

---

## 8. Technology Stack

| ชั้น | เทคโนโลยี | เหตุผล |
|---|---|---|
| Frontend | React 18 + TypeScript, Vite, TanStack Query, Zustand, Tailwind + shadcn/ui, **RJSF** (react-jsonschema-form) | RJSF render ฟอร์มกฎอัตโนมัติจาก paramSchema |
| Canvas overlay | Konva.js / react-konva | วาด bounding box บน preview |
| API Gateway | Traefik หรือ Kong | TLS termination, rate limit, JWT validation |
| Backend (orchestration/CRUD) | **Go 1.22+** — Echo/Fiber, sqlc, `nats.go` | concurrency สูง, latency ต่ำ, binary เล็ก |
| Backend (AI/CV) | **Python 3.11+** — FastAPI, OpenCV, Pillow, scikit-image, PaddleOCR/Tesseract, PyThaiNLP, LanguageTool, ONNX Runtime | ecosystem AI/CV ครบ |
| Message Broker | **NATS.io + JetStream** | persistence, at-least-once, dedup, lightweight |
| Object Storage | **RustFS** (S3 Compatible) | presigned URL, event notification → NATS |
| Database | PostgreSQL 16 (ต่อ service มี schema แยก) | ACID + JSONB สำหรับ params/findings |
| Cache | Redis / NATS KV | session, idempotency key, rule manifest cache |
| Search | PostgreSQL FTS (Phase 1) → OpenSearch (Phase 2) | ค้นหา finding/specimen |
| Deploy | Docker + Kubernetes, Helm, ArgoCD | GitOps |
| Observability | OpenTelemetry, Prometheus, Grafana, Loki, Tempo | |

---

## 9. Data Model (หลัก)

```sql
-- project-svc
projects(id, tenant_id, code, name, description, default_ruleset_id,
         status, created_by, created_at, archived_at)
project_members(project_id, user_id, role, joined_at)
specimens(id, tenant_id, project_id, name, folder_path, tags[],
          current_revision, created_at, deleted_at)
specimen_revisions(id, specimen_id, revision, object_key, checksum_sha256,
                   mime_type, size_bytes, width_px, height_px, dpi,
                   page_count, uploaded_by, uploaded_at)

-- ruleset-svc
rule_types(rule_type PK, version, display_name, engine, subject,
           param_schema JSONB, supported_mime_types[], default_severity,
           timeout_sec, last_seen_at, status)           -- self-registered
rule_sets(id, tenant_id, project_id, name, version, status, approved_by,
          approved_at, created_at)                       -- immutable per version
rule_set_items(id, rule_set_id, rule_type, order_no, enabled, severity,
               blocking, params JSONB)

-- inspection-svc
inspection_jobs(id, tenant_id, project_id, specimen_id, specimen_revision,
                rule_set_id, rule_set_version, rule_set_snapshot JSONB,
                status, verdict, verdict_reason, decided_by, decided_at,
                started_at, finished_at)
check_tasks(id, job_id, rule_type, rule_id, state, attempt, error_code,
            checker_name, checker_version, model_revision,
            duration_ms, started_at, finished_at)
findings(id, job_id, task_id, defect_type, severity, confidence,
         region JSONB, message, evidence_key,
         adjudication, adjudicated_by, adjudicated_at, reason)
outbox(id, aggregate_id, event_type, payload JSONB, published_at)

-- index สำคัญ
CREATE INDEX ON inspection_jobs (tenant_id, project_id, status, created_at DESC);
CREATE INDEX ON findings (job_id, severity);
CREATE INDEX ON check_tasks (job_id) WHERE state <> 'DONE';
```

---

## 10. API Specification (ตัวอย่างสำคัญ)

| Method | Endpoint | คำอธิบาย |
|---|---|---|
| `POST` | `/api/v1/projects` | สร้าง Project |
| `GET` | `/api/v1/projects/{id}/specimens?tag=&folder=&page=` | รายการชิ้นงาน |
| `POST` | `/api/v1/projects/{id}/uploads:presign` | ขอ presigned PUT URL |
| `GET` | `/api/v1/rule-types` | รายการ Rule Type ที่ลงทะเบียนอยู่ + paramSchema |
| `POST` | `/api/v1/projects/{id}/rule-sets` | สร้าง Rule Set version ใหม่ |
| `PATCH` | `/api/v1/rule-sets/{id}/items` | เพิ่ม/ลบ/เรียงกฎ (สร้าง draft version) |
| `POST` | `/api/v1/rule-sets/{id}/publish` | อนุมัติใช้งาน |
| `POST` | `/api/v1/rule-sets/{id}/dry-run` | ทดลองรันกับ specimen ตัวอย่าง |
| `POST` | `/api/v1/inspections` | สั่งตรวจ (single / batch) → `202 Accepted` + jobId |
| `GET` | `/api/v1/inspections/{jobId}` | สถานะ + findings |
| `GET` | `/api/v1/inspections/{jobId}/events` | SSE stream ความคืบหน้า |
| `POST` | `/api/v1/findings/{id}:adjudicate` | Confirm / Dismiss |
| `POST` | `/api/v1/inspections/{jobId}/verdict:override` | กลับผล พร้อมเหตุผล |
| `GET` | `/api/v1/reports/projects/{id}/summary?from=&to=` | สรุปผล |

**Convention:** ทุก POST ที่เปลี่ยนสถานะต้องส่ง header `Idempotency-Key`

---

## 11. Sequence — End-to-End Flow

```
1. User → project-svc            : POST /uploads:presign
2. project-svc → User            : { url, objectKey, expiresIn: 900 }
3. User → RustFS                 : PUT ไฟล์โดยตรง (multipart ถ้าไฟล์ใหญ่)
4. RustFS → NATS                 : qc.storage.object.created
5. intake-svc                    : verify → scan → extract metadata → thumbnail
6. intake-svc → NATS             : qc.intake.specimen.ingested.v1
7. inspection-svc                : load Rule Set → snapshot → create Job + N CheckTasks
8. inspection-svc → NATS         : fan-out → qc.check.{spelling|color|logo|dimension|filesize}.v1
9. checker-*                     : presigned GET (stream/range) → execute → findings
10. checker-* → NATS             : qc.inspection.task.completed.v1  (Msg-Id = taskId)
11. inspection-svc               : fan-in → ครบทุก task → VerdictPolicyService
12. inspection-svc → NATS        : qc.inspection.job.evaluated.v1
13. notify-svc / report-svc      : แจ้งเตือน + อัปเดต read model
14. UI (SSE)                     : แสดง verdict + overlay findings
15. Inspector                    : adjudicate → qc.review.verdict.overridden.v1 (ถ้ามี)
```

---

## 12. Acceptance Criteria (ตัวอย่าง)

| ID | Given / When / Then |
|---|---|
| AC-01 | **Given** Rule Set มี 5 กฎเปิดใช้งาน **When** อัปโหลดภาพ PNG **Then** เกิด Inspection Job ที่มี 5 CheckTask และจบภายใน 30 วินาที |
| AC-02 | **Given** deploy `checker-font` ตัวใหม่ **When** service announce manifest **Then** รายการกฎใน UI แสดง "ตรวจฟอนต์" พร้อมฟอร์มพารามิเตอร์ โดยไม่ deploy frontend/inspection-svc ใหม่ |
| AC-03 | **Given** `checker-logo` ล่ม **When** Job รัน **Then** กฎอื่นยังได้ผลปกติ และ Job สถานะ `PARTIAL` ระบุ task ที่ค้าง |
| AC-04 | **Given** NATS ส่ง message ซ้ำ **When** checker รับซ้ำ **Then** ไม่เกิด Finding ซ้ำในฐานข้อมูล |
| AC-05 | **Given** ไฟล์ 120 MB เกิน `maxSizeMB = 100` **When** ตรวจ **Then** Finding severity = `CRITICAL` และ Verdict = `FAILED` |
| AC-06 | **Given** แก้ Rule Set เป็น v2 **When** เปิดดู Job เก่า **Then** ยังแสดงผลอิงตาม v1 ที่ใช้ตอนตรวจ |
| AC-07 | **Given** User ของ Project A **When** เรียก `GET /inspections/{jobOfProjectB}` **Then** ได้ `404` |

---

## 13. สมมติฐานและข้อจำกัด

- RustFS รองรับ S3 Event Notification ปลายทางเป็น NATS ได้โดยตรง — หากยังไม่รองรับ ต้องมี `storage-bridge` service ทำ webhook → NATS แทน
- Phase 1 ตรวจได้เฉพาะ raster image + PDF; ไฟล์ native (AI, PSD, Figma) ต้อง export เป็นภาพก่อน
- ความแม่นยำการตรวจสะกดภาษาไทยขึ้นกับคุณภาพ OCR — ต้องมี custom dictionary ต่อ Project
- การตรวจสีควรระบุ color profile (sRGB/CMYK) เพราะ ΔE จะคลาดเคลื่อนถ้า profile ไม่ตรง
