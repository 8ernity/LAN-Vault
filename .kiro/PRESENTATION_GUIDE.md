# LANVault - Hackathon Presentation Guide
## GenAI Sprint 1.0 - Track 3 (Build with Kiro)

**Team**: Singularity  
**Date**: November 2026  
**Submission Deadline**: November 4, 2026, 11:59 PM IST

---

## Presentation Requirements

**Format**: Single presentation (PPT or PDF)  
**Length**: ~10 slides  
**Demo**: No separate recording needed (include screenshots)

### Required Content

1. ✅ The problem and who it's for
2. ✅ Your solution and how it works
3. ✅ How you used Kiro (specs, steering, hooks - screenshots required)
4. ✅ Screenshots of the working project
5. ✅ Impact and what you'd build next

---

## Slide-by-Slide Breakdown

### Slide 1: Title & Team
**Title**: LANVault - Secure Offline File Transfer  
**Subtitle**: Built with Kiro's Spec-Driven Workflow  
**Team**: Singularity  
**Hackathon**: GenAI Sprint 1.0 - Track 3

**Visuals**:
- App icon/logo
- Team name prominently displayed
- AWS SBG and Kiro logos

---

### Slide 2: The Problem

**Headline**: File Transfer Between PC & Phone Is Broken

**Pain Points** (with icons):
- 📶 Cloud apps require internet & burn mobile data
- 💰 Size limits and slow uploads
- 🔌 USB cables are flaky (MTP issues)
- 📱 Ad-supported apps compromise privacy
- 🔄 Large transfers fail midway with no resume

**Target Users**:
- **Riya** (20, Student): Shares lecture notes on campus WiFi
- **Karan** (27, Video Editor): Transfers 4K clips (10-50 GB)
- **Meera** (45, Shop Owner): Moves product photos & invoices

**Key Quote**: *"I lost a 45-minute upload at 90% because my data ran out"* - Student beta tester

---

### Slide 3: Our Solution

**Headline**: LANVault - Offline by Design

**Core Features** (2 columns):

**Left Column - Privacy & Security**:
- ✅ 100% offline (zero internet required)
- 🔐 Encrypted with TLS 1.3
- 🤝 QR code pairing (one-time)
- 🚫 No ads, no accounts, no tracking

**Right Column - Performance & Reliability**:
- ⚡ Fast transfers (85%+ of WiFi speed)
- 📦 Unlimited file sizes
- 🔄 Pause & resume anytime
- 📁 Folder structure preserved

**The Pitch**: *Transfer anything, anywhere with WiFi, without internet*

**Visual**: Simple before/after comparison or feature grid

---

### Slide 4: How It Works

**Simple 4-Step Flow** (with numbered icons):

1. **Discover** 🔍
   - Both devices on same WiFi/hotspot
   - Auto-discovery via mDNS + beacon

2. **Pair** 🤝
   - Scan QR code once
   - Mutual TLS authentication

3. **Send** 📤
   - Select files/folders
   - Choose recipient
   - Real-time progress

4. **Receive** 📥
   - Accept or decline
   - See sender & size first
   - Files saved securely

**Visual**: Flow diagram with device icons and arrows

---

### Slide 5: Technical Architecture

**Headline**: Built on Open Protocol & Modern Stack

**Architecture Diagram** (simplified):
```
┌─────────────────────────────────────┐
│     Android App (Kotlin/Compose)    │
├──────────────┬────────────┬─────────┤
│  Discovery   │  Protocol  │ Transfer│
│   (mDNS)     │  Handler   │  Engine │
└──────────────┴────────────┴─────────┘
         │           │            │
┌────────▼───────────▼────────────▼───┐
│    TLS 1.3 Mutual Authentication    │
└─────────────────────────────────────┘
```

**Key Technologies**:
- **Protocol**: Custom wire protocol v1.1 (documented)
- **Security**: ECDSA P-256, TLS 1.3, SHA-256 chunks
- **Android**: Jetpack Compose, Material 3, Coroutines
- **Storage**: Scoped Storage (SAF), Room database
- **Network**: mDNS service discovery, UDP beacon fallback

**Stats**:
- 15 Kotlin files
- 2,500+ lines of code
- 20+ protocol message types
- 100% offline operation

---

### Slide 6: How Kiro Helped - Requirements Phase

**Headline**: Spec-Driven Development with Kiro

**Screenshot**: `.kiro/lanvault-spec.md` Requirements section showing:
- Structured user stories table
- NFR (non-functional requirements) with targets
- Acceptance criteria for each feature

**Kiro's Role**:
1. **Structured Requirements**
   - Converted 60-page PRD into actionable user stories
   - Defined clear acceptance criteria
   - Prioritized features (P0, P1, P2)

2. **Comprehensive Documentation**
   - Auto-generated from protocol specifications
   - Cross-referenced with technical docs
   - Maintained consistency across 1000+ requirements

**Quote Box**: 
*"Kiro helped transform a complex protocol specification into clear, implementable tasks with measurable outcomes"*

**Example User Story Highlight**:
```
US-06: As a sender, I want real-time progress with speed and ETA
Acceptance: Live updates every second, EMA-smoothed speed, accurate ETA
Status: ✅ Implemented
```

---

### Slide 7: How Kiro Helped - Design & Tasks

**Headline**: From Design to Implementation

**Two-Column Layout**:

**Left: Design Phase**
- Screenshot of architecture diagram from spec
- Component breakdown (Discovery, Protocol, Transfer Engine)
- Data model definitions

**Right: Task Breakdown**
- Screenshot of Phase breakdown (10 phases, 50+ tasks)
- Checkbox progress tracking
- Clear dependencies

**Kiro Workflow**:
```
Requirements → Design → Task Breakdown → Implementation → Testing
        ↓           ↓           ↓              ↓              ↓
   User Stories  Architecture  Phases    Code Generation   Validation
```

**Task Example**:
```
Phase 3: Protocol Implementation ✅
├─ T3.1: Frame encoding/decoding ✅
├─ T3.2: HELLO/HELLO_OK handlers ✅
├─ T3.3: OFFER/MANIFEST/ACCEPT flow ✅
├─ T3.4: PAIR_REQUEST/PAIR_OK ✅
└─ T3.7: Keepalive (PING/PONG) ✅
```

---

### Slide 8: How Kiro Helped - Steering Files

**Headline**: Kiro Steering: Context-Aware Development

**Screenshot**: Show `.kiro/steering/protocol-implementation.md` with highlighted sections

**Three Steering Files Created**:

1. **protocol-implementation.md**
   - Frame encoding rules (big-endian, UTF-8)
   - Protocol constants reference
   - Test vector validation

2. **security-requirements.md**
   - Cryptographic standards
   - Trust model guidelines
   - Secure coding practices

3. **android-best-practices.md**
   - MVVM architecture patterns
   - Compose best practices
   - Scoped storage guidelines

**How Steering Helped**:
- ✅ Automatic context injection when editing relevant files
- ✅ Consistent coding standards across 15+ Kotlin files
- ✅ Protocol compliance verification
- ✅ Security requirements always visible

**Code Example with Steering**:
```kotlin
// Steering: Always use BIG_ENDIAN for protocol frames
ByteBuffer.allocate(size).order(ByteOrder.BIG_ENDIAN)
           .putInt(length)
           .put(frameType)
           .put(payload)
```

---

### Slide 9: Working Project Screenshots

**Headline**: LANVault in Action

**4-Panel Screenshot Grid**:

**Panel 1: Home Screen**
- Shows nearby devices
- Network status chip
- Visibility indicator
- Material 3 design

**Panel 2: QR Pairing**
- QR code display
- "Scan to pair" instruction
- Device fingerprint shown

**Panel 3: Transfer in Progress**
- Progress bar at 67%
- Live speed: "15.3 MB/s"
- ETA: "23 seconds remaining"
- Pause/Cancel buttons

**Panel 4: Transfer History**
- List of completed transfers
- File counts and sizes
- Success/Failed status indicators
- Timeline with timestamps

**Caption for each**: Brief description of what's shown

---

### Slide 10: Impact & Next Steps

**Headline**: Built for Impact, Ready to Scale

**What We've Achieved**:
- ✅ Fully functional Android app
- ✅ Open protocol specification (documented)
- ✅ Secure by design (TLS 1.3, no internet)
- ✅ Resume-capable transfers
- ✅ 2,500+ lines of production code

**Real Impact**:
- **Students**: Share large files on campus without data charges
- **Creators**: Transfer 4K videos without cloud delays
- **Privacy-conscious**: No tracking, no third-party servers

**What's Next** (Roadmap):
1. **v1.1 (P1)**: Windows desktop app with Go + Wails
2. **Performance**: Optimize for 95%+ TCP throughput
3. **Testing**: End-to-end transfer tests, chaos testing
4. **WiFi Direct**: Eliminate need for router/hotspot (P2)
5. **Cross-platform**: macOS, Linux, iOS investigation

**The Vision**:
*Making offline-first file sharing the default, not the workaround*

**Call to Action**:
- GitHub: github.com/your-repo/lanvault (open source)
- Protocol: Documented for interoperability
- Community: Welcoming contributions

---

## Key Presentation Tips

### Visual Design
- Use consistent color scheme (Material 3 palette)
- Include LANVault app screenshots on every slide (watermark or sidebar)
- Use icons for bullet points
- Keep text minimal, visuals prominent

### Kiro Evidence (Critical for Judging)
Make sure these are clearly visible:
1. **Spec document** (requirements, design, tasks)
2. **Steering files** (at least one full screenshot)
3. **File structure** showing `.kiro/` directory
4. **Before/after** Kiro assistance (e.g., generated boilerplate)

### Story Flow
- Problem → Solution → How We Built It → Impact
- Show progression: Requirements → Design → Tasks → Code → Working App
- Emphasize Kiro at every stage

### Technical Credibility
- Reference actual file names and code
- Show real protocol details (not mock-ups)
- Include metrics (2,500 LOC, 15 files, 20+ message types)

### Demo Strategy
Since no video required:
- Use annotated screenshots
- Show progression (pairing → transfer → completion)
- Include error states (pause, resume)

---

## Judging Criteria Alignment

### Problem & Idea (20 points)
- **Slide 2**: Clear problem statement with real user personas
- **Differentiator**: Offline-first is unique and well-justified

### Use of Kiro (25 points) ⭐ CRITICAL
- **Slides 6-8**: Three full slides on Kiro usage
- **Evidence**: Spec document, steering files, task breakdown
- **Workflow**: Show complete spec-driven process

### Solution Quality (15 points)
- **Slide 5**: Technical architecture
- **Slide 9**: Working screenshots prove functionality
- **Protocol**: Documented, testable, interoperable

### Presentation (15 points)
- **Design**: Clean, professional, consistent
- **Flow**: Logical progression, easy to follow
- **Clarity**: Technical but accessible

### Surprise Criteria (25 points)
- **Open Protocol**: Documented for anyone to implement
- **Security**: Proper TLS 1.3, no shortcuts
- **Resume**: Chunk-level with integrity checks
- **Testing**: Test vectors for compliance

---

## Assets Needed

### Screenshots to Capture
1. ✅ Home screen with nearby devices
2. ✅ QR code pairing screen
3. ✅ File picker with multi-select
4. ✅ Transfer in progress (with live stats)
5. ✅ Paused transfer with resume button
6. ✅ Completed transfer in history
7. ✅ Device management screen
8. ✅ Spec document (requirements section)
9. ✅ Spec document (task breakdown with checkboxes)
10. ✅ Steering file (protocol-implementation.md)
11. ✅ Project structure showing `.kiro/` folder

### Diagrams to Create
1. Architecture diagram (Android app layers)
2. Protocol flow (discovery → pair → transfer)
3. 4-step user flow
4. Kiro workflow diagram

### Code Snippets
1. Frame encoding with steering reference
2. TLS configuration
3. Chunk verification

---

## Presentation Checklist

### Content
- [ ] All 10 slides complete
- [ ] Problem clearly stated with personas
- [ ] Solution features listed with benefits
- [ ] Technical architecture diagram included
- [ ] Kiro spec document screenshot (requirements)
- [ ] Kiro spec document screenshot (tasks)
- [ ] Kiro steering file screenshot
- [ ] At least 4 app screenshots
- [ ] Impact statement with metrics
- [ ] Next steps roadmap

### Kiro Evidence (CRITICAL)
- [ ] Spec file shown (`.kiro/lanvault-spec.md`)
- [ ] Steering files shown (at least 1)
- [ ] Task breakdown with checkboxes visible
- [ ] Clear before/after Kiro assistance
- [ ] Workflow diagram showing spec → design → tasks

### Technical Credibility
- [ ] Real file names and paths
- [ ] Actual code snippets
- [ ] Protocol details mentioned
- [ ] Metrics included (LOC, files, messages)

### Design
- [ ] Consistent color scheme
- [ ] Professional typography
- [ ] Icons used effectively
- [ ] Screenshots high quality
- [ ] No clutter, clean layout

### Story
- [ ] Logical flow start to finish
- [ ] Kiro's role emphasized throughout
- [ ] Impact clearly communicated
- [ ] Vision for future stated

---

## Backup Slides (Optional)

### Technical Deep Dive
- Protocol frame format details
- Chunk bitmap encoding
- Path validation rules
- Resume state integrity

### Code Examples
- Identity.kt (certificate generation)
- Protocol.kt (frame handling)
- TransferEngine.kt (chunk streaming)

### Test Results
- Frame encoding test vectors passed
- Manifest hash validation
- Protocol conformance tests

---

## Final Tips

1. **Practice the Story**: Know how to explain each slide in 60 seconds
2. **Emphasize Kiro**: 25 points on Kiro usage - make it count!
3. **Show, Don't Tell**: Screenshots > descriptions
4. **Be Specific**: "2,500 LOC, 15 files" > "lots of code"
5. **Connect to Impact**: Every feature ties back to user problems

**Remember**: Judges want to see:
- Real working project ✅
- Clear use of Kiro's spec workflow ✅
- Thoughtful design and implementation ✅
- Potential for impact ✅

---

**Good luck! 🚀**
