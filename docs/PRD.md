# LANVault — Product Requirements Document

Version 1.2 · Corrected draft · 1 November 2026 · Companion specification: PROTOCOL.md (wire protocol v1.1; included)

## 1. Document overview

**Purpose.** This document defines what LANVault is, who it is for, what every screen and feature does, how it should look and behave, and the technical approach for building it. It is the single product reference for design, development, testing and release.

**Audience.** The developer(s), reviewers, hackathon judges or mentors, and testers.

Relationship to PROTOCOL.md. This PRD says what the product does and why. PROTOCOL.md says how two devices talk on the wire. Wherever this PRD mentions a transfer behavior (chunk size, ports, timeouts, resume), the number comes from docs/protocol-constants.json, and PROTOCOL.md wins on any wire-level detail. Implementation gate: the protocol specification, versioned constants, design tokens, settings schema and both-language test vectors MUST be present in the repository and reviewed before protocol implementation. Companion artifacts are included in the same versioned source bundle. The application release (v1.0) is distinct from the wire protocol version (v1.1). Any missing or disagreeing artifact blocks implementation; never infer wire messages or cryptographic behavior.

**Conventions.**

- Priority: **P0** = must ship in v1.0. **P1** = v1.1 fast follow. **P2** = v2 or later.

- IDs: FR-\<AREA\>-\<nn\> functional requirement, NFR-\<nn\> non-functional requirement, US-\<nn\> user story, SCR-\<nn\> screen, UF-\<nn\> user flow.

- Single source of truth: protocol values (ports, sizes, timeouts) live in protocol-constants.json; design values (colors, spacing, radii, motion) live in design-tokens.json. This PRD names them and does not redefine them. Where a current value is quoted for readability, the JSON file is authoritative.

- MUST, SHOULD and MAY follow RFC 2119.

**Document map.**

| Part | Sections | Content                                                                           |
|------|----------|-----------------------------------------------------------------------------------|
| 1    | 1–7      | Overview, goals, users, scope, platforms, navigation                              |
| 2    | 8–10     | Tab-by-tab feature specs (Windows, Android) and system-level features             |
| 3    | 11–14    | UI design system, user flows, error catalogue, settings reference                 |
| 4    | 15–26    | Tech stack, architecture, data model, security, quality, release, risks, appendix |

## 2. Product overview

### 2.1 Summary

LANVault is a fully offline, secure file-sharing application for moving files and folders between Windows PCs and Android phones over a local network. Two devices that share a Wi-Fi network or a mobile hotspot (and, later, a Wi-Fi Direct link) find each other automatically, pair once, then send anything of any size with encryption, pause and resume, and real-time speed and progress.

The desktop app is built with Go, Wails, React and TypeScript. The Android app is native Kotlin. Both implement the same open protocol (PROTOCOL.md).

### 2.2 Vision

Sending a file to a device in the same room should be instant, private and free of accounts, ads, uploads and mobile data. LANVault is the fast, secure, no-nonsense alternative to ad-supported sharing apps.

### 2.3 Problem statement

- Moving files between a Windows PC and an Android phone usually means a USB cable (slow to set up, MTP is flaky), a cloud upload (needs internet, burns mobile data, has size limits), or an ad-supported sharing app (ads, trackers, bundled content, and in some cases cloud features the user never asked for).

- Students, creators and shop owners often work on networks with no internet or with expensive data. They still have a perfectly good local network between their own devices.

- Large transfers fail halfway and must restart from zero. Folders with thousands of files are slow and error-prone.

- Users cannot easily tell whether a transfer is private, or why it is not connecting.

### 2.4 Positioning

Qualitative comparison with the categories of alternatives (not specific products):

| Capability               | Cloud / messaging apps | Ad-supported sharing apps | USB cable (MTP) | **LANVault**                 |
|--------------------------|------------------------|---------------------------|-----------------|------------------------------|
| Works with no internet   | No                     | Usually                   | Yes             | **Yes, always**              |
| Uses mobile data         | Yes                    | Sometimes                 | No              | **Never**                    |
| Ads / bundled content    | Varies                 | Common                    | No              | **None**                     |
| Typical file size limits | Often                  | Varies                    | Filesystem only | **Filesystem only**          |
| Pause and resume         | Varies                 | Rare                      | No              | **Yes, chunk-level**         |
| Folders                  | Varies                 | Varies                    | Yes             | **Yes, structure preserved** |
| Encrypted in transit     | Varies                 | Varies                    | n/a             | **Yes, mutual TLS 1.3**      |
| Wireless                 | Yes                    | Yes                       | No              | **Yes**                      |

### 2.5 Key differentiators

1.  Offline by construction: no cloud, no account, no analytics, no outbound internet traffic.

2.  Security that is verifiable: mutual TLS with pinned fingerprints, QR pairing, explicit accept.

3.  Resume that works: chunk-level bitmaps survive disconnects, app restarts and device sleep.

4.  Honest diagnostics: when a connection fails, the app explains why (AP isolation, firewall, different network) and what to try.

5.  Open protocol: documented and testable, so the two apps cannot drift apart.

### 2.6 Product principles

| Principle               | What it means in practice                                                                      |
|-------------------------|------------------------------------------------------------------------------------------------|
| Offline by construction | No feature may require, or silently attempt, internet access                                   |
| Private by default      | Receiving is off or ask-first by default; nothing is shared without pairing or explicit accept |
| Fast without tuning     | Parallel streams and sensible defaults; no speed settings needed for normal use                |
| Honest about limits     | Show real reasons for failure and real speeds, never fake progress                             |
| Minimal UI              | One primary action per screen, few settings, no clutter                                        |
| Always resumable        | Any interrupted transfer can be continued without starting over                                |
| No surprises            | The receiver always sees who is sending, how much, and where it will be saved                  |

## 3. Goals, non-goals and success metrics

### 3.1 Goals

| ID  | Goal                                                                                                                         |
|-----|------------------------------------------------------------------------------------------------------------------------------|
| G1  | Transfer files and folders between Windows and Android with no internet connection                                           |
| G2  | Work over same Wi-Fi and over either device's mobile hotspot in v1; auto-join via QR in v1.1; Wi-Fi Direct as a stretch goal |
| G3  | Impose no app-level size limit; the only limit is the destination filesystem                                                 |
| G4  | Encrypt every transfer and authenticate both devices                                                                         |
| G5  | Pause, resume and recover from interruptions without re-sending completed data                                               |
| G6  | Discover nearby devices automatically, with QR and manual fallbacks                                                          |
| G7  | Show accurate real-time speed, progress and ETA                                                                              |
| G8  | Provide a modern, minimal UI on both platforms                                                                               |
| G9  | Ship with no ads, no cloud storage, no account and no telemetry                                                              |

### 3.2 Non-goals (v1)

| ID  | Non-goal                                      | Reason                                                      |
|-----|-----------------------------------------------|-------------------------------------------------------------|
| NG1 | iOS, macOS and Linux clients                  | Separate platform work; core and UI stay portable for later |
| NG2 | Internet or relay transfers                   | Contradicts the offline premise                             |
| NG3 | Cloud backup, sync or storage                 | Not a sync tool                                             |
| NG4 | Continuous folder sync                        | One-shot transfers only                                     |
| NG5 | Multi-recipient broadcast                     | One sender to one receiver per transfer in v1               |
| NG6 | Accounts, profiles, social features           | No identity beyond a device certificate                     |
| NG7 | Malware scanning or content moderation        | Receiver accepts explicitly; OS security handles the rest   |
| NG8 | Silent control of Wi-Fi or hotspot on Android | Not permitted by Android since version 10                   |
| NG9 | Ads, monetization, in-app purchases           | Core product promise                                        |

### 3.3 Success metrics

The app has no telemetry, so metrics come from automated benchmark suites, chaos tests and moderated usability sessions.

| ID  | Metric                 | Target                                                                                                                              | Measured by                              |
|-----|------------------------|-------------------------------------------------------------------------------------------------------------------------------------|------------------------------------------|
| M1  | First-transfer success | At least 90% of test users finish a first transfer within 5 minutes of install, unassisted                                          | Usability sessions with 10 or more users |
| M2  | Integrity              | 100% of files byte-identical (SHA-256) across the integrity suite of at least 1,000 transfers, including one file of 10 GiB or more | Automated test run                       |
| M3  | Resume reliability     | At least 99% of kill-and-resume tests complete with correct data (kill connection, kill process, toggle Wi-Fi, sleep device)        | Automated chaos tests                    |
| M4  | Throughput             | At least 85% of measured raw TCP throughput on the same link                                                                        | Benchmark against a plain TCP baseline   |
| M5  | Discovery              | Median time to first peer of 3 seconds or less on home Wi-Fi; at least 95% of lab networks work through mDNS or the beacon fallback | Network lab matrix                       |
| M6  | Startup                | Desktop cold start to interactive in 2 seconds or less; Android 1.5 seconds or less, on reference devices                           | Manual and scripted timing               |
| M7  | Offline guarantee      | Zero connections to non-local addresses during a full usage session                                                                 | Packet capture review                    |
| M8  | Stability              | At least 99.5% crash-free sessions during beta                                                                                      | Local crash logs shared by beta testers  |

## 4. Users and use cases

### 4.1 Personas

| Persona                               | Profile                                                                                                       | Needs                                                  | Pain today                                                                            |
|---------------------------------------|---------------------------------------------------------------------------------------------------------------|--------------------------------------------------------|---------------------------------------------------------------------------------------|
| **Riya**, 20, engineering student     | Moves lecture PDFs, project folders and screen recordings between laptop and phone on campus and hostel Wi-Fi | Fast, free, works without data, handles folders        | Data plan limits, cable hassle, ads in sharing apps, client isolation on campus Wi-Fi |
| **Karan**, 27, freelance video editor | Sends 4K clips of 10 to 50 GB from phone to PC and back                                                       | Resume, integrity, visible speed, no size cap          | Cloud uploads take hours; transfers fail at 90%                                       |
| **Meera**, 45, small shop owner       | Moves product photos and invoices between a shop PC and phone                                                 | Very simple flow, large buttons, plain-language errors | Gets lost in complex apps; worried about privacy                                      |

### 4.2 User stories

| ID    | As a...  | I want...                                                          | So that...                                       | Priority |
|-------|----------|--------------------------------------------------------------------|--------------------------------------------------|----------|
| US-01 | New user | the app to find my other device automatically                      | I do not type IP addresses                       | P0       |
| US-02 | New user | to pair two devices once by scanning a QR code                     | future transfers are trusted and secure          | P0       |
| US-03 | Sender   | to drag files and folders onto the app and pick a device           | sending takes seconds                            | P0       |
| US-04 | Sender   | to share from the Android share sheet                              | I can send from any app                          | P0       |
| US-05 | Receiver | to see who is sending, how much, and accept or decline             | nothing arrives without my consent               | P0       |
| US-06 | Receiver | received files saved to a folder I choose                          | I can find them easily                           | P0       |
| US-07 | Sender   | live speed, progress and time remaining                            | I know how long it will take                     | P0       |
| US-08 | Sender   | to pause and resume a transfer                                     | I can free up the network or battery temporarily | P0       |
| US-09 | Sender   | interrupted transfers to continue from where they stopped          | I never restart a large file from zero           | P0       |
| US-10 | Sender   | folder structure and file dates preserved                          | the files look the same on the other side        | P0       |
| US-11 | User     | a clear explanation when connection fails                          | I can fix it myself                              | P0       |
| US-12 | User     | a warning before sending a file the destination cannot hold        | I do not waste time on a transfer that must fail | P0       |
| US-13 | User     | transfer history with status                                       | I can check what was sent or received            | P0       |
| US-14 | User     | to manage and remove trusted devices                               | I control who can send to me                     | P0       |
| US-15 | User     | dark and light themes                                              | the app is comfortable at any hour               | P0       |
| US-16 | User     | the phone to join my PC hotspot (or the reverse) through a QR code | I do not type Wi-Fi passwords                    | P1       |
| US-17 | User     | to send text and links, not only files                             | quick snippets are easy                          | P1       |
| US-18 | User     | Send with LANVault in the Windows Explorer right-click menu        | I can send without opening the app               | P1       |
| US-19 | User     | to limit transfer speed                                            | the transfer does not saturate my network        | P1       |
| US-20 | User     | Wi-Fi Direct connections                                           | I can transfer with no router or hotspot at all  | P2       |

## 5. Scope and release plan

| Release       | Theme                | Included features                                                                                                                                                                                                                                                                                                                                                                         |
|---------------|----------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| **v1.0 (P0)** | Core offline sharing | Discovery (mDNS, beacon, manual), QR pairing, trusted devices, mutual TLS 1.3, send and receive files and folders in both directions, parallel streams, chunk hashing, pause, resume, cancel, speed and progress, history, firewall setup, system tray and notifications, Android foreground service, share-sheet target, dark and light themes, diagnostics, FAT32 and disk-space checks |
| **v1.1 (P1)** | Convenience          | Hotspot join via QR, send text and links, Explorer context menu, speed limit, Windows webcam QR scanning, short-code (SAS) pairing, localization framework, and optional per-trusted-device auto-accept (off by default; never for unpaired senders).                                                                                                                                     |
| **v2.0 (P2)** | Reach                | Wi-Fi Direct, macOS and Linux desktop builds, iOS investigation, multi-receiver send, ARM64 Windows build                                                                                                                                                                                                                                                                                 |

Implementation sequence (without reducing P0 scope): first prove Go-to-Go transfer and crash-safe resume with a CLI; then complete discovery and authenticated pairing; then Windows UI; then Android receive/send interoperability; finally platform packaging and usability. Each milestone requires executable integration tests. Mocked UI screens are not evidence that file transfer works.

**Out of scope for every release in this document:** cloud features, accounts, sync, ads, telemetry.

## 6. Platforms, environments and constraints

### 6.1 Platform support

| Platform                        | Support in v1 | Notes                                                                                                                                                                                                                             |
|---------------------------------|---------------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| Windows 11 (x64)                | Supported     | Mica backdrop where available                                                                                                                                                                                                     |
| Windows 10 21H2 and later (x64) | Supported     | Requires WebView2. The offline Windows installer MUST bundle or include the Microsoft Evergreen Standalone Runtime where WebView2 is missing; an online bootstrapper alone is insufficient. Pre-installed WebView2 may be reused. |
| Windows on ARM64                | P2            | Go and Wails support it; needs test hardware                                                                                                                                                                                      |
| Android 10 (API 29) and later   | Supported     | Phones and tablets; portrait and landscape. minSdk 29 gives built-in TLS 1.3 and a simpler Wi-Fi API story                                                                                                                        |
| Android 9 and earlier           | Not supported | Would need a bundled TLS provider and legacy Wi-Fi code paths                                                                                                                                                                     |
| macOS, Linux, iOS               | Not supported | See non-goals                                                                                                                                                                                                                     |

### 6.2 Network scenarios

| Scenario                                                | Release       | How devices connect                                             | Notes                                                                                                                                                                                              |
|---------------------------------------------------------|---------------|-----------------------------------------------------------------|----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| Same Wi-Fi router                                       | P0            | Both join the same network                                      | Fails if the router has client or AP isolation; diagnostics explain this                                                                                                                           |
| Android hotspot, PC joined                              | P0            | User turns hotspot on, PC joins it                              | Phone hotspot may operate with mobile data OFF. Resolve and use the hotspot-local interface/IP; verify PC-to-phone and phone-to-PC transfers without a WAN route.                                  |
| Windows Mobile Hotspot, phone joined                    | P0            | User turns on Mobile Hotspot in Windows, phone joins            | Windows hotspot interface must be discovered and advertised correctly; Android must connect through that local link even if cellular data is available. Validate both with cellular data disabled. |
| Ethernet plus Wi-Fi                                     | P0            | PC on Ethernet, phone on the same router's Wi-Fi                | Works only if the router bridges wired and wireless clients and allows peer traffic; blocked discovery does not necessarily imply blocked unicast.                                                 |
| Auto-join via QR                                        | P1            | QR carries SSID and password; Android uses WifiNetworkSpecifier | Android shows a system confirmation dialog                                                                                                                                                         |
| Wi-Fi Direct                                            | P2            | Direct peer link                                                | Windows WinRT APIs are awkward from Go; adapter support varies                                                                                                                                     |
| Different subnets, VLANs, guest networks with isolation | Not supported | n/a                                                             | Diagnose blocked peer connectivity without asserting a single cause: test IP/subnet, routing, firewall, multicast and AP/client isolation, then suggest local hotspot or manual address.           |

### 6.3 Known constraints

- **Speed is set by the radio and storage.** 2.4 GHz links are slow; 5 GHz or Wi-Fi 6 links are far faster. The app must never be the bottleneck, but cannot exceed the link.

- **No store-and-forward.** Both devices must be online on the same link at the same time.

- **Android background limits.** On Android 15+ with targetSdk 35+, dataSync foreground services have a shared six-hour allowance per rolling 24-hour window while the app is backgrounded, subject to platform reset conditions. Doze, OEM policies and background-start restrictions may also interrupt transfers. Do NOT promise an indefinite background receiver. A user-started bounded receive session and active transfer use the permitted service lifecycle; handle onTimeout(), stop promptly, persist progress, and offer a user-initiated Resume. Evaluate user-initiated data transfer jobs where their platform and use-case limitations permit.

- **Android local network permission.** When targeting Android 17 / API 37 or later, LANVault MUST declare and request ACCESS_LOCAL_NETWORK at runtime before direct local-socket discovery/transfer. When targeting API 36 or lower, do not declare or request that permission; follow current platform migration guidance. Handle denied, revoked and pre-granted states, including tests with mobile data off.

- **Scoped storage.** Writes go through SAF or MediaStore. Large-folder enumeration is slower than raw file access.

- **Filesystem limits.** FAT32 allows 4 GiB per file; names legal on one OS may be illegal on the other; Windows has a 260-character path limit unless extended-length paths are used.

- **Windows Firewall.** First listen triggers a prompt on Public profile networks; the installer adds a rule for Private networks.

- **Unsigned binaries.** SmartScreen and antivirus warnings are likely without a code-signing certificate.

- **Encryption protects data in transit only.** Files are plaintext on both devices.

## 7. Information architecture and navigation

### 7.1 Navigation model

Both apps use the same five primary destinations so documentation, support and muscle memory carry across platforms.

| Tab           | Purpose                                 | One-line description                            |
|---------------|-----------------------------------------|-------------------------------------------------|
| **Home**      | See nearby devices and start a transfer | The dashboard: who is around, and quick actions |
| **Send**      | Choose what to send and to whom         | Pick files or folders, review, choose a device  |
| **Receive**   | Be ready to receive                     | Visibility, my QR code, incoming requests       |
| **Transfers** | Watch and manage transfers              | Active transfers with live stats, and history   |
| **Devices**   | Manage trust                            | Paired devices, pair a new one                  |

Settings is a secondary destination (sidebar footer on Windows, gear icon in the top bar on Android). About and Diagnostics live under Settings.

**Windows:** left sidebar (collapsed 72 px with icons, expanded 220 px with labels), content area on the right, status footer at the bottom of the sidebar.

**Android:** Material 3 bottom navigation with the five tabs, top app bar with the title, network chip and gear icon. Tablets and landscape use a navigation rail.

### 7.2 Global UI elements

| Element                 | Where                                              | Behavior                                                                                                                                              |
|-------------------------|----------------------------------------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------|
| Network chip            | Top bar (both)                                     | Shows the active network name or interface and local IP. Tap or click opens Diagnostics. Turns amber when no usable network is found                  |
| Visibility indicator    | Top bar and tray (Windows), top bar (Android)      | Shows Visible, Receive-only, or Hidden. Tap toggles                                                                                                   |
| Transfer mini-bar       | Bottom of content (above bottom nav on Android)    | Appears while any transfer is active. Shows aggregate progress and speed. Click or tap opens Transfers                                                |
| Incoming request dialog | Modal over any tab                                 | Appears when a device wants to send. Cannot be missed on Windows (also raises a toast); on Android it is a heads-up notification plus an in-app sheet |
| Toasts and snackbars    | Bottom right (Windows), above bottom nav (Android) | Non-blocking confirmations and errors with an optional action                                                                                         |
| Empty states            | Every list                                         | Illustration, one-sentence explanation, one primary action                                                                                            |

### 7.3 Screen inventory

| ID     | Screen                               | Windows        | Android                                     |
|--------|--------------------------------------|----------------|---------------------------------------------|
| SCR-01 | Onboarding: welcome                  | Yes            | Yes                                         |
| SCR-02 | Onboarding: device name              | Yes            | Yes                                         |
| SCR-03 | Onboarding: permissions and firewall | Yes (firewall) | Yes (notifications, nearby devices, camera) |
| SCR-04 | Onboarding: save location            | Yes            | Yes                                         |
| SCR-05 | Home                                 | Yes            | Yes                                         |
| SCR-06 | Send: choose items                   | Yes            | Yes                                         |
| SCR-07 | Send: review and choose device       | Yes            | Yes                                         |
| SCR-08 | Receive                              | Yes            | Yes                                         |
| SCR-09 | Incoming request dialog or sheet     | Yes            | Yes                                         |
| SCR-10 | Transfers: active list               | Yes            | Yes                                         |
| SCR-11 | Transfer detail                      | Yes            | Yes                                         |
| SCR-12 | Transfers: history list              | Yes            | Yes                                         |
| SCR-13 | Devices: trusted list                | Yes            | Yes                                         |
| SCR-14 | Pair: show my QR                     | Yes            | Yes                                         |
| SCR-15 | Pair: scan QR                        | P1 (webcam)    | Yes                                         |
| SCR-16 | Pair: enter address manually         | Yes            | Yes                                         |
| SCR-17 | Device detail                        | Yes            | Yes                                         |
| SCR-18 | Settings (with subpages)             | Yes            | Yes                                         |
| SCR-19 | Diagnostics: Can't connect?          | Yes            | Yes                                         |
| SCR-20 | About and licenses                   | Yes            | Yes                                         |

## 8. Windows app: tab-by-tab specification

### 8.0 Application shell

| Item           | Specification                                                                                                    |
|----------------|------------------------------------------------------------------------------------------------------------------|
| Framework      | Wails v2 desktop shell, Go backend, React + TypeScript frontend in WebView2                                      |
| Window         | Default 1100 × 720, minimum 900 × 600; remembers size, position and maximized state; per-monitor DPI aware       |
| Instances      | Single instance. A second launch focuses the running window and forwards any file arguments to it                |
| Title bar      | Native title bar in v1. Mica backdrop on Windows 11 where supported, solid surface color elsewhere               |
| Close behavior | Setting: Close button exits, or minimizes to tray (default: minimize to tray, with a one-time hint)              |
| Startup        | Optional launch at sign-in, optionally minimized to tray                                                         |
| Sleep          | While a transfer is active, the app prevents system sleep (setting, default on) and releases the block when idle |

**Keyboard shortcuts**

| Shortcut              | Action                                                                          |
|-----------------------|---------------------------------------------------------------------------------|
| Ctrl+1 to Ctrl+5      | Switch to Home, Send, Receive, Transfers, Devices                               |
| Ctrl+,                | Open Settings                                                                   |
| Ctrl+O / Ctrl+Shift+O | Add files / add folder (Send tab)                                               |
| Ctrl+R or F5          | Rescan for nearby devices                                                       |
| F1                    | Open Diagnostics                                                                |
| Space                 | Pause or resume the focused transfer                                            |
| Delete                | Cancel the focused transfer (confirmation) or remove the focused history record |
| Esc                   | Close dialog or sheet; decline nothing implicitly                               |
| Ctrl+Q                | Quit completely (also closes the tray icon)                                     |

### 8.1 Home tab (SCR-05)

**Purpose.** Show who is nearby and make the first action obvious.

**Layout.** Top: this-device card (name, platform icon, IP, visibility pill). Middle: Nearby devices grid. Bottom: Recent transfers (last 5). Quick actions row: Send files, Send folder, Show my QR.

**Device card.** Platform icon, device name, online dot, trust badge (Paired or Not paired), small copyable address, last seen. Primary button Send. Overflow menu: Pair, Details, Copy address, Open Diagnostics for this device. Dropping files on a card sends to that device.

| ID         | Requirement                                                                                                                                                                                  | Priority |
|------------|----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|----------|
| FR-HOME-01 | Show devices discovered by mDNS and the UDP beacon, merged by device_id, updating live                                                                                                       | P0       |
| FR-HOME-02 | Remove a device after 3 missed beacon intervals or an mDNS goodbye or TTL expiry                                                                                                             | P0       |
| FR-HOME-03 | Sort paired first, then most recently used, then name                                                                                                                                        | P0       |
| FR-HOME-04 | Search box appears when more than 6 devices are listed                                                                                                                                       | P1       |
| FR-HOME-05 | Drag and drop files or folders onto a device card to start the Send flow with the device preselected                                                                                         | P0       |
| FR-HOME-06 | Connect by address button opens SCR-16 for manual ip:port entry                                                                                                                              | P0       |
| FR-HOME-07 | Rescan button restarts discovery and shows a scanning indicator                                                                                                                              | P0       |
| FR-HOME-08 | Empty state after 10 seconds without any device: three checks (same network, other device open and ready to receive, try a hotspot) with buttons Diagnostics, Show my QR, Connect by address | P0       |
| FR-HOME-09 | Choosing Send on an unpaired device that rejects unpaired senders shows a Pair first prompt                                                                                                  | P0       |
| FR-HOME-10 | Recent transfers list shows the last 5 with state, peer and size; click opens detail                                                                                                         | P0       |
| FR-HOME-11 | On network interface change, re-advertise and rescan within 2 seconds                                                                                                                        | P0       |
| FR-HOME-12 | Banner when no usable network exists: Not connected to a network. Connect to Wi-Fi or turn on Mobile Hotspot                                                                                 | P0       |

### 8.2 Send tab (SCR-06, SCR-07)

The Send tab is a three-step flow on one page: **Choose items**, **Choose device**, **Review and send**.

**Step 1: Choose items.** Large drop zone plus Add files and Add folder buttons. Below it, the selected-items list: icon, name, size (folders show file count and total once scanned), remove button. A totals bar shows item count, file count, total size and scan status. Clear all button.

**Step 2: Choose device.** List of nearby devices and paired-but-offline devices (greyed, not selectable). Connect by address link.

**Step 3: Review and send.** One-line summary, primary Send button, collapsible Advanced section (parallel streams: Auto; follow symlinks: off; include hidden files: on).

| ID         | Requirement                                                                                                                                                    | Priority |
|------------|----------------------------------------------------------------------------------------------------------------------------------------------------------------|----------|
| FR-SEND-01 | Add multiple files and folders by drag and drop or native pickers                                                                                              | P0       |
| FR-SEND-02 | Scan folders recursively in the background; show live file count and bytes; the UI stays responsive on trees of 100,000 or more files                          | P0       |
| FR-SEND-03 | Preserve relative paths and modification times; include empty folders                                                                                          | P0       |
| FR-SEND-04 | Remove duplicates when the same path is added twice                                                                                                            | P0       |
| FR-SEND-05 | Warn about unreadable, locked or too-long paths; offer Skip or Retry                                                                                           | P0       |
| FR-SEND-06 | Skip symlinks and special files by default (opt-in to follow symlinks)                                                                                         | P0       |
| FR-SEND-07 | Send button enabled only with at least one item and one reachable device                                                                                       | P0       |
| FR-SEND-08 | After Send, go to Transfers with the new transfer in Waiting for acceptance state                                                                              | P0       |
| FR-SEND-09 | If a transfer to the same device is already running, queue the new one and show its position                                                                   | P0       |
| FR-SEND-10 | Warn when the item count exceeds 100,000 (slower, more memory) but allow it                                                                                    | P0       |
| FR-SEND-11 | Build the manifest at send time (snapshot). A file that changes during transfer fails with FILE_CHANGED; other files continue and the transfer ends as Partial | P0       |
| FR-SEND-12 | Send again from history with the same selection                                                                                                                | P0       |
| FR-SEND-13 | Text and link tab in the picker for short snippets                                                                                                             | P1       |

**Edge cases.** Selected item deleted before Send: remove it and show a toast. Device disappears before Send: keep selection, show Device is no longer reachable with Rescan. Zero-byte files are valid and sent as manifest entries only.

### 8.3 Receive tab (SCR-08)

**Purpose.** Make this PC ready to receive and show how others can reach it.

**Layout.** A status card (Ready to receive, with the visibility mode selector), an identity card (device name editable inline, short fingerprint, address list with copy buttons), a pairing QR card (QR code, countdown ring until it refreshes, Refresh now), a Save to row (path, Change, Open), a pending-requests list, and the last 5 received transfers with Open and Show in folder.

**Visibility modes**

| Mode                        | Behavior                                                                                           |
|-----------------------------|----------------------------------------------------------------------------------------------------|
| Everyone nearby (default)   | Advertised by mDNS and beacon whenever the app is running                                          |
| Only while this tab is open | Advertised only while the Receive tab is visible                                                   |
| Hidden                      | Not advertised. Reachable by QR, manual address, or by paired devices using the last known address |

| ID         | Requirement                                                                                                                                                                                                                                             | Priority |
|------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|----------|
| FR-RECV-01 | Start the listener at app launch on DEFAULT_PORT; if busy, pick the next free port and advertise the real one                                                                                                                                           | P0       |
| FR-RECV-02 | Visibility modes as above, persisted                                                                                                                                                                                                                    | P0       |
| FR-RECV-03 | Pairing QR with one-time token, refreshed automatically every PAIR_TOKEN_TTL seconds, with a visible countdown                                                                                                                                          | P0       |
| FR-RECV-04 | Show all eligible local addresses (excluding loopback, link-local and known virtual adapters unless selected in Settings)                                                                                                                               | P0       |
| FR-RECV-05 | Choose and open the save folder; default is the Downloads folder plus a LANVault subfolder                                                                                                                                                              | P0       |
| FR-RECV-06 | In application v1.0, show an incoming request dialog for every transfer, including paired devices; no silent acceptance. Optional explicitly enabled paired-device auto-accept is P1 (application v1.1).                                                | P0       |
| FR-RECV-07 | Per-trusted-device auto-accept is P1 only (application v1.1), OFF by default, and NEVER permitted for unpaired senders. Application v1.0 always requires explicit approval.                                                                             | P1       |
| FR-RECV-08 | Only one active incoming transfer. If another sender offers a transfer, return BUSY with a retry hint; optionally show a non-actionable busy event, but do not claim the request is queued unless an actual bounded queue is implemented and specified. | P0       |
| FR-RECV-09 | Pre-checks before showing the dialog: free space, destination filesystem file-size limit, path validity                                                                                                                                                 | P0       |
| FR-RECV-10 | Do not disturb for 1 hour: decline all unpaired requests silently                                                                                                                                                                                       | P1       |
| FR-RECV-11 | Open file and Show in folder actions from received items                                                                                                                                                                                                | P0       |

**Incoming request dialog (SCR-09).** Content, top to bottom:

1.  Sender name and platform icon; badge Paired, or a warning Not paired with the short fingerprint (grouped, for example ab12 cd34) to verify.

2.  Summary: file count, folder count, total size.

3.  Preview of the first 5 items with sizes, and an expandable tree.

4.  Destination folder and free space.

5.  Warnings: files that will be renamed, low space, FAT32 limit.

6.  Buttons: **Accept** (primary), **Decline**; for unpaired senders also **Accept and trust**.

A request not answered within OFFER_RESPONSE_TIMEOUT_S (300 seconds, defined in the product section of protocol-constants.json) is declined automatically and the sender sees Not answered.

### 8.4 Transfers tab (SCR-10, SCR-11, SCR-12)

A segmented control switches between **Active** and **History**.

**Active list row.** Direction icon (up for send, down for receive), peer name, title (file name or folder name plus count), state pill, progress bar, a line reading done of total, speed, ETA, and actions Pause or Resume, Cancel, Expand.

**State pills.** Waiting for acceptance, Connecting, Transferring, Paused, Reconnecting, Verifying, Finishing, Completed, Completed with errors, Failed, Cancelled, Interrupted.

**Transfer detail (SCR-11).**

- Header: direction, peer, start time.

- Large progress bar with percentage and exact bytes.

- Live speed graph: last 60 seconds, updated every 500 ms.

- Stats: current speed, average speed, peak speed, elapsed, ETA, files done of total, chunks done of total, streams in use, retries, peer address and interface.

- Virtualized file list with per-file status and progress.

- Collapsible event log (for example: Reconnected after 3 s, Resumed 412 chunks skipped).

- Actions: Pause or Resume, Cancel, Retry failed files, Open folder (receiver), Copy details.

**History (SCR-12).** Grouped by day, filterable (All, Sent, Received; status), searchable. Row: name, peer, size, date and time, duration, average speed, status. Row actions: Open, Show in folder, Send again, Remove record (never deletes files).

| ID         | Requirement                                                                                                                                                                 | Priority |
|------------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------|----------|
| FR-XFER-01 | Speed is smoothed with an exponential moving average over about 2 seconds; ETA is remaining bytes divided by smoothed speed; ETA shows a dash until 3 seconds of data exist | P0       |
| FR-XFER-02 | Progress counts only chunks acknowledged as verified and persisted, never bytes merely sent                                                                                 | P0       |
| FR-XFER-03 | Pause and resume from either side; a paused state is shown on both devices                                                                                                  | P0       |
| FR-XFER-04 | Cancel with confirmation. Receiver choice: Keep partial data to resume later, or Delete partial data                                                                        | P0       |
| FR-XFER-05 | Automatic reconnect with backoff (1, 2, 4, up to 30 seconds) showing Reconnecting and the attempt number; manual Resume now button                                          | P0       |
| FR-XFER-06 | After an app restart, interrupted sends appear as Interrupted with a Resume button; interrupted receives show Waiting for the sender for up to PARTIAL_RETENTION_DAYS       | P0       |
| FR-XFER-07 | Show a Verified badge on completed files after hash checks                                                                                                                  | P0       |
| FR-XFER-08 | Failures show a reason in plain language and a next action (Section 13)                                                                                                     | P0       |
| FR-XFER-09 | Retry only the failed files of a Partial transfer                                                                                                                           | P0       |
| FR-XFER-10 | History retention setting: 30 days, 90 days, or forever; Clear history                                                                                                      | P0       |
| FR-XFER-11 | Completion toast and optional sound                                                                                                                                         | P0       |
| FR-XFER-12 | Copy transfer details (a text report without file paths if the user chooses) for support                                                                                    | P0       |
| FR-XFER-13 | Speed limit per transfer                                                                                                                                                    | P1       |

**Units.** The protocol uses bytes. The Windows app formats sizes with 1024-based units labelled KB, MB, GB to match Windows Explorer; the Android app uses 1000-based units to match the Android system Files app. Speed is shown in MB/s by default, with an optional Mbps display in Settings. Each platform has one formatter function covered by unit tests.

### 8.5 Devices tab (SCR-13 to SCR-17)

Trusted devices list. Row: platform icon, name (with local nickname if set), short fingerprint, paired date, last seen, online status. Actions: Send files, Rename (local nickname only), Auto-accept toggle (P1; unavailable in app v1.0), Details, Remove. Primary button: Pair new device.

**Pair new device (SCR-14, SCR-15).** Tabs **Show my QR** and **Scan QR** (webcam, P1), with numbered instructions on both devices. On success a confirmation shows the other device's name and short fingerprint with the line Both devices now trust each other.

**Connect by address (SCR-16).** Fields for IP and port, Connect button. Used for sending to a device that cannot be discovered. It works for paired devices, and for unpaired devices whose owner allows unpaired senders.

Device detail (SCR-17). Full fingerprint in monospace with Copy, first paired, last connected, last IP, transfer count and total bytes, auto-accept toggle (P1; unavailable in app v1.0), nickname, Remove device. Removal explains that the device must pair again and discards resumable partial data from it.

| ID        | Requirement                                                                                                                                  | Priority |
|-----------|----------------------------------------------------------------------------------------------------------------------------------------------|----------|
| FR-DEV-01 | List, rename (nickname), and remove trusted devices                                                                                          | P0       |
| FR-DEV-02 | QR pairing as in PROTOCOL.md section 4.1: the displaying device shows the QR, the scanning device completes pairing                          | P0       |
| FR-DEV-03 | Show full and short fingerprints; allow copy                                                                                                 | P0       |
| FR-DEV-04 | Optional per-trusted-device auto-accept; off by default, paired devices only (application v1.1). Not present or enabled in application v1.0. | P1       |
| FR-DEV-05 | Failed pairing shows the exact cause (expired code, wrong network, fingerprint mismatch)                                                     | P0       |
| FR-DEV-06 | Rate-limit pairing: invalidate the QR token after 3 failed attempts                                                                          | P0       |
| FR-DEV-07 | Scan QR using the webcam                                                                                                                     | P1       |
| FR-DEV-08 | Short-code (SAS) pairing as an alternative to QR                                                                                             | P1       |

In v1, Windows displays the QR and Android scans it. Pairing is mutual, so both devices trust each other afterward.

### 8.6 Settings (SCR-18)

Settings pages and every option are listed in Section 14. Behavior rules:

- Changes apply immediately; no Save button.

- Risky actions (Regenerate identity, Clear all data) require a confirmation dialog that states the consequence.

- A Restore defaults button resets everything except identity and trusted devices.

### 8.7 Diagnostics: Can't connect? (SCR-19)

A guided checklist that runs live checks and explains the result in plain language.

| Check                                                   | Pass                                                                                                | Fail message and action                                                                                                                                                                                                         |
|---------------------------------------------------------|-----------------------------------------------------------------------------------------------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| Network connected                                       | An eligible interface with a private IPv4 address exists                                            | Not connected. Join Wi-Fi or turn on a hotspot                                                                                                                                                                                  |
| Listener running                                        | Port is open locally                                                                                | Could not start listening. Choose another port in Settings                                                                                                                                                                      |
| Firewall rule (Windows)                                 | Inbound rule exists and network profile is Private                                                  | Windows Firewall may block incoming connections. Button: Fix firewall (UAC prompt)                                                                                                                                              |
| mDNS advertising                                        | Service registered                                                                                  | Discovery may be blocked. Use QR or address                                                                                                                                                                                     |
| Beacon sending                                          | Datagrams leave on the interface                                                                    | Broadcast blocked. Use QR or address                                                                                                                                                                                            |
| Peer reachability (select a device or enter an address) | TCP connect, TLS handshake and HELLO succeed; latency shown                                         | Connection or authenticated HELLO failed. Check peer IP, local-network permission, listener, firewall and certificate/pairing status; if peer traffic is blocked, try a hotspot. Do not label all failures as client isolation. |
| Multiple adapters or VPN                                | Single active LAN interface                                                                         | VPN or virtual adapter detected. Select the correct interface in Settings                                                                                                                                                       |
| Android LAN permission                                  | For targetSdk 37+, local network access granted (or permitted by a suitable system-mediated picker) | Permission denied or revoked. Explain why LANVault needs local device access, offer Request permission or Open app settings; do not silently retry sockets.                                                                     |

Buttons: Run again, Copy report, Export report (a text file with no file names).

## 9. Android app: tab-by-tab specification

### 9.0 Application shell

| Item                 | Specification                                                                                                                                |
|----------------------|----------------------------------------------------------------------------------------------------------------------------------------------|
| UI toolkit           | Jetpack Compose, Material 3, single-activity, edge-to-edge, predictive back                                                                  |
| Navigation           | Bottom navigation with 5 tabs on phones; navigation rail on tablets and in landscape; top app bar with title, network chip and Settings gear |
| Orientation          | Portrait and landscape; large-screen layouts follow window size classes                                                                      |
| Back behavior        | From a root tab, Back moves the app to the background and never kills an active transfer                                                     |
| Theme                | Follow system, or force Light or Dark; brand colors from design tokens; dynamic color is optional and off by default                         |
| minSdk and targetSdk | minSdk 29; targetSdk is the latest stable level required at release time                                                                     |

### 9.1 Home

Same content as Windows Home, adapted: device cards in a vertical list, pull-to-refresh, extended Send floating button that opens the Send flow. Additional Android requirements:

| ID             | Requirement                                                                                                                                                                                                                                                                                                      | Priority |
|----------------|------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|----------|
| FR-AND-HOME-01 | Network banner: if the phone has only mobile data, show LANVault needs Wi-Fi or a hotspot, with a button to open Wi-Fi settings                                                                                                                                                                                  | P0       |
| FR-AND-HOME-02 | Select the reachable local Network/interface with ConnectivityManager and bind client sockets to it; handle Wi-Fi station, Android-hosted hotspot, Windows-hosted hotspot and Ethernet-bridged peers separately. Never silently fall back to cellular/WAN routes. Test each direction with mobile data disabled. | P0       |
| FR-AND-HOME-03 | Hold a MulticastLock while discovering or advertising                                                                                                                                                                                                                                                            | P0       |
| FR-AND-HOME-04 | All FR-HOME requirements of Section 8.1 apply, except window-specific drag and drop                                                                                                                                                                                                                              | P0       |
| FR-AND-HOME-05 | When targetSdk \>=37 on Android 17+, gate mDNS, UDP discovery and TCP/other LAN sockets behind ACCESS_LOCAL_NETWORK; handle denied/revoked permission and provide Settings recovery. For targetSdk \<=36, do not request the new permission.                                                                     | P0       |

### 9.2 Send

**Sources.** Files (system document picker, multi-select), Photos and videos (system photo picker, no storage permission needed), Folder (document tree picker), Text and link (P1).

**Flow.** Pick sources, see a list with thumbnails for images and videos, name and size, remove buttons, and a totals bar. Tap Choose device to open a bottom sheet with nearby and paired devices. Tap Send to start; the app moves to Transfers.

| ID             | Requirement                                                                                                                                                               | Priority |
|----------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------|----------|
| FR-AND-SEND-01 | Register as a share target for single and multiple items of any type (ACTION_SEND and ACTION_SEND_MULTIPLE). Shared items open the Send tab preselected                   | P0       |
| FR-AND-SEND-02 | Request persistable read access where the picker offers it, so interrupted sends can resume after the process is killed                                                   | P0       |
| FR-AND-SEND-03 | If URI access was lost, show Re-select the files to resume; the receiver's partial data is kept and reused when the same files are selected again (manifest hash matches) | P0       |
| FR-AND-SEND-04 | Enumerate folders with direct content-resolver queries rather than per-file DocumentFile calls, to keep large trees fast                                                  | P0       |
| FR-AND-SEND-05 | Preserve modification times where the provider reports them; otherwise use the transfer time and mark the file in history                                                 | P0       |
| FR-AND-SEND-06 | All FR-SEND requirements of Section 8.2 apply                                                                                                                             | P0       |

### 9.3 Receive

Same content as Windows Receive, plus:

| ID             | Requirement                                                                                                                                                                                                                                                                                                                                                                                                                                                                   | Priority |
|----------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|----------|
| FR-AND-RECV-01 | Listener is active while the app is in the foreground                                                                                                                                                                                                                                                                                                                                                                                                                         | P0       |
| FR-AND-RECV-02 | An accepted, user-initiated transfer may continue after leaving the foreground only through an OS-permitted, time-bounded dataSync foreground lifecycle; persist resume state, handle timeout, and stop the service after work completes. App v1.0 does NOT promise background idle discovery/listening. An optional explicitly started standby receive window (Off/15/60 minutes) is P1 and requires separate OS-policy review; never use a perpetual idle dataSync service. | P0       |
| FR-AND-RECV-03 | In v1.0, all incoming requests are shown while the app is foreground. If a P1 standby receive window is implemented and permitted by the OS, requests may trigger Accept/Decline notifications only while that user-started window is active and notification permission is granted; otherwise return a clear receiver-unavailable error.                                                                                                                                     | P1       |
| FR-AND-RECV-04 | Save location is a user-chosen folder (document tree permission) or the default Downloads/LANVault folder through MediaStore                                                                                                                                                                                                                                                                                                                                                  | P0       |
| FR-AND-RECV-05 | Write through ParcelFileDescriptor to a temporary name, then rename on completion                                                                                                                                                                                                                                                                                                                                                                                             | P0       |
| FR-AND-RECV-06 | When the app is backgrounded with no permitted listening service/window, sender receives an explicit receiver-unavailable/connection error; diagnostics say the receiver must be opened or a user-initiated receive session started. Never claim guaranteed background reachability.                                                                                                                                                                                          | P0       |
| FR-AND-RECV-07 | Quick Settings tile to toggle receiving                                                                                                                                                                                                                                                                                                                                                                                                                                       | P1       |

### 9.4 Transfers

Same structure as Windows (Active and History, detail screen, graph, file list, log), as cards with swipe-to-remove on history records. In addition:

- A **progress notification** per active transfer with Pause and Cancel actions, updated at most once per second.

- A **completion notification** with Open and Show in folder actions.

- An Interrupted card appears when the service timed out or the process was killed, with Resume.

### 9.5 Devices

Same as Windows Devices. Pairing on Android defaults to scanning: **Scan QR** opens a full-screen camera view (CameraX with ZXing decoding, torch toggle, permission rationale). Show my QR is the second option. QR from the gallery is P1.

### 9.6 Android system behavior and permissions

| Capability         | Requirement                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
|--------------------|----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| Foreground service | Use foregroundServiceType=dataSync only for permitted user-initiated receive windows and active transfer work, not indefinite idle listening. Android 15+ targetSdk 35+ has a six-hour total background budget per 24 hours for dataSync services. Implement Service.onTimeout(int,int), persist verified chunks, call stopSelf() promptly, notify where permitted, and resume only via an allowed user action. Test budget exhaustion, background start restrictions and OEM kills. Assess user-initiated data transfer jobs separately if appropriate. |
| Locks              | WifiLock and a partial WakeLock held only while a transfer is active                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| Storage            | SAF and MediaStore only; no all-files access permission                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| Battery            | A Settings screen explains OEM battery optimization and links to the system battery settings; the app never asks for exemption automatically                                                                                                                                                                                                                                                                                                                                                                                                             |
| Process death      | All transfer state lives in Room; on restart, active transfers become Interrupted                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |

| Permission                                       | Why                                                                                                                                                                                                                                  | When requested                                                                               |
|--------------------------------------------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|----------------------------------------------------------------------------------------------|
| INTERNET                                         | Required by Android for any socket, including local ones; the app makes no internet connections                                                                                                                                      | Install time                                                                                 |
| ACCESS_NETWORK_STATE, ACCESS_WIFI_STATE          | Detect network type and local address                                                                                                                                                                                                | Install time                                                                                 |
| CHANGE_WIFI_MULTICAST_STATE                      | Multicast lock for mDNS                                                                                                                                                                                                              | Install time                                                                                 |
| NEARBY_WIFI_DEVICES (never for location)         | Android 13+ Wi-Fi management features that specifically require this permission (for example P1 hotspot join); not a substitute for ACCESS_LOCAL_NETWORK on targetSdk 37+. Use neverForLocation where permitted by the API contract. | When first needed (hotspot join, P1)                                                         |
| ACCESS_LOCAL_NETWORK (only if targetSdk \>=37)   | Android 17/API 37+ runtime permission for direct LAN socket access, including mDNS/UDP and TCP. Do not declare/request when targeting API \<=36; allow Android 16 opt-in compatibility testing.                                      | Before first LAN discovery/connection when required; handle denial, revocation and pre-grant |
| CAMERA                                           | Scan pairing QR                                                                                                                                                                                                                      | On first Scan QR                                                                             |
| POST_NOTIFICATIONS                               | Progress and request notifications on Android 13 and later                                                                                                                                                                           | During onboarding, with a rationale                                                          |
| FOREGROUND_SERVICE, FOREGROUND_SERVICE_DATA_SYNC | Keep transfers alive                                                                                                                                                                                                                 | Install time                                                                                 |
| WAKE_LOCK                                        | Keep CPU awake during transfer                                                                                                                                                                                                       | Install time                                                                                 |

The app does not request location, contacts, microphone, or broad storage permissions.

## 10. System-level and cross-platform features

### 10.1 Onboarding (SCR-01 to SCR-04)

| Step | Screen                   | Content                                                                                                                                                                                                                                                                                                |
|------|--------------------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| 1    | Welcome                  | Logo, tagline Share files between your devices. No internet. No ads. Three short value points; Get started                                                                                                                                                                                             |
| 2    | Device name              | Prefilled from the OS device name; editable; explained as what other devices will see                                                                                                                                                                                                                  |
| 3    | Permissions and firewall | Windows: explain Private-network firewall access. Android: request notifications when appropriate; explain nearby Wi-Fi access only when needed, and, on targetSdk 37+, request ACCESS_LOCAL_NETWORK before LAN operations. Explain denial and Settings recovery; never ask for unrelated permissions. |
| 4    | Save location            | Show the default folder; Change; Finish                                                                                                                                                                                                                                                                |
| End  | Next step                | Prompt: Pair your first device, opening the Devices tab with the QR ready                                                                                                                                                                                                                              |

Onboarding runs once and can be re-opened from Settings, About.

### 10.2 Notifications

| Event                          | Windows                                        | Android                                                 |
|--------------------------------|------------------------------------------------|---------------------------------------------------------|
| Incoming request               | Toast with Accept and Decline, plus tray flash | High-priority notification with Accept and Decline      |
| Transfer progress              | Mini-bar in app; taskbar progress              | Ongoing low-priority notification with Pause and Cancel |
| Transfer complete              | Toast with Open and Show in folder             | Notification with Open and Show in folder               |
| Transfer failed or interrupted | Toast with reason and Resume                   | Notification with reason and Resume                     |
| Pairing complete               | In-app toast                                   | In-app snackbar                                         |

Notification channels on Android: Incoming requests (high), Transfer progress (low), Results (default). Users can mute each in system settings.

### 10.3 System tray (Windows)

- Icon states: idle, active (animated), paused, attention (needs response).

- Left click opens or hides the window. Right-click menu: Open LANVault, Visibility (Everyone nearby, Hidden), Pause all transfers, Quit.

- Tooltip shows current aggregate speed while a transfer is active.

### 10.4 Send integration

| Integration                                                         | Platform | Priority |
|---------------------------------------------------------------------|----------|----------|
| Share sheet target for files                                        | Android  | P0       |
| Drag and drop of files and folders onto the window and device cards | Windows  | P0       |
| Explorer right-click Send with LANVault                             | Windows  | P1       |
| Command-line interface: send a path to a named device, list devices | Windows  | P1       |
| Direct Share shortcuts for frequent devices                         | Android  | P1       |

### 10.5 Filesystem handling and safety

- Receiver validation and sanitization follow PROTOCOL.md section 10, including global file/directory collision checks after NFC normalization, Windows sanitization and case folding (including implicit directories). The receipt screen lists any renamed items (for example, a file renamed because its name contains a colon, which Windows does not allow).

- A name collision never overwrites: the file is saved as name (1), name (2), and so on.

- Disk-space watch: if free space falls below the remaining bytes plus a safety margin during a transfer, pause and show Low disk space with Resume after freeing space.

- FAT32 and exFAT destinations are detected; a file of 4 GiB or more on FAT32 is rejected before the transfer begins, naming the file.

- Partial files use a temporary name and are never visible under the final name until verified.

### 10.6 Accessibility

| Area               | Requirement                                                                                                                                              |
|--------------------|----------------------------------------------------------------------------------------------------------------------------------------------------------|
| Standard           | Target WCAG 2.2 level AA                                                                                                                                 |
| Contrast           | Text 4.5:1 or better, large text and UI components 3:1 or better, in both themes                                                                         |
| Keyboard (Windows) | Every control reachable and operable; visible focus ring; logical tab order                                                                              |
| Screen readers     | Narrator on Windows and TalkBack on Android: labels on every control; progress announced at coarse intervals (every 10 percent) rather than continuously |
| Motion             | Respect the system reduced-motion setting; no essential information conveyed by animation                                                                |
| Touch              | Targets of 48 dp or more on Android; 36 px or more on Windows                                                                                            |
| Text scaling       | Android layouts remain usable at 200 percent font scale                                                                                                  |
| Color              | Never the only cue: statuses always pair color with an icon and text                                                                                     |

### 10.7 Localization

English only in v1, with every user-visible string externalized (translation files on desktop, string resources on Android), logical (start and end) layout properties so right-to-left languages can be added, and locale-aware number and date formatting. Additional languages are P1.

### 10.8 Privacy

- No accounts, analytics, advertising identifiers, crash reporters that upload, or remote configuration.

- Logs stay on the device. Users can export a diagnostic report manually; it excludes file names unless the user opts in.

- Device name and ID are visible to other devices on the local network, as described in the Security section.

## 11. UI design system

### 11.1 Visual language

Calm, spacious and minimal. Flat surfaces separated by thin borders, one accent color, generous spacing, rounded corners, and motion only where it explains a change. The app is designed dark-first and fully supports light. The desktop and Android apps share one token file, design-tokens.json; a build script generates the CSS variables and Tailwind theme for React, and the Compose theme for Android. No color, size or duration is typed by hand in UI code.

### 11.2 Design tokens

**Color.** Values below are the current contents of design-tokens.json. A CI check computes the contrast ratios from the token values (text on its background at least 4.5:1, UI components at least 3:1) and fails the build if any pair is below target.

| Token           | Dark     | Light    | Usage                            |
|-----------------|----------|----------|----------------------------------|
| bg.base         | \#0E1116 | \#F6F8FA | App background                   |
| bg.surface      | \#151A22 | \#FFFFFF | Cards, panels                    |
| bg.raised       | \#1C232D | \#F0F3F7 | Inputs, hover, dialogs           |
| border.subtle   | \#2A3340 | \#E1E6EC | Dividers, card borders           |
| text.primary    | \#E8EDF3 | \#101828 | Main text                        |
| text.secondary  | \#9AA7B5 | \#475467 | Supporting text                  |
| text.disabled   | \#5B6675 | \#98A2B3 | Disabled text                    |
| accent.default  | \#4C8DFF | \#2563EB | Primary actions, active progress |
| accent.hover    | \#6AA1FF | \#1D4ED8 | Hover and pressed                |
| accent.onAccent | \#0B1220 | \#FFFFFF | Text on accent                   |
| success         | \#34D399 | \#047857 | Completed, verified              |
| warning         | \#FBBF24 | \#B45309 | Paused, reconnecting, caution    |
| danger          | \#F87171 | \#DC2626 | Failed, destructive actions      |

**Typography.**

| Role    | Size / line height                     | Weight | Notes                          |
|---------|----------------------------------------|--------|--------------------------------|
| Display | 28 / 36                                | 600    | Onboarding, empty states       |
| Title   | 20 / 28                                | 600    | Page titles                    |
| Section | 16 / 24                                | 600    | Card and section headings      |
| Body    | 14 / 20 on desktop, 16 / 24 on Android | 400    | Default text                   |
| Caption | 12 / 16                                | 400    | Secondary details              |
| Mono    | 13 / 20                                | 500    | Fingerprints, addresses, ports |

Desktop typeface: Inter (bundled variable font) with Segoe UI Variable and system-ui as fallbacks; mono: Cascadia Mono with a system monospace fallback. Android: Roboto (system default) and the system monospace.

**Spacing, shape and motion.**

| Group                   | Tokens                                                                                                                       |
|-------------------------|------------------------------------------------------------------------------------------------------------------------------|
| Spacing (4-point scale) | 4, 8, 12, 16, 24, 32, 48                                                                                                     |
| Radius                  | control 8, card 12, dialog and sheet 16, pill 999                                                                            |
| Elevation               | Cards: border only. Dialogs: one soft shadow. In dark theme, raised surfaces use a lighter surface color instead of a shadow |
| Motion duration         | micro 120 ms, panel 200 ms, page 300 ms                                                                                      |
| Motion easing           | One standard curve for all transitions                                                                                       |
| Reduced motion          | When the system asks for reduced motion, replace movement with a plain fade or no animation                                  |

### 11.3 Core components

| Component               | Specification                                                                                                                                                                                                                          |
|-------------------------|----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| Button                  | Primary (accent fill), Secondary (border), Ghost (text), Danger (danger fill). Height 36 px on desktop, 48 dp on Android. One primary button per view                                                                                  |
| Device card             | Platform icon, name, online dot, trust badge, address caption, primary Send. Hover raises to bg.raised; drop target highlights with an accent border                                                                                   |
| Transfer card           | Direction icon, peer, title, state pill, progress bar, stats line, actions                                                                                                                                                             |
| Progress bar            | 8 px, rounded. Accent while transferring, warning when paused, success on completion, danger on failure. Indeterminate animation only for Waiting and Connecting                                                                       |
| Speed graph             | 60-second sparkline in the accent color with a faint area fill; peak marker; no axis clutter                                                                                                                                           |
| State pill              | Waiting and Connecting: neutral. Transferring, Verifying, Finishing: accent. Paused, Reconnecting, Interrupted: warning. Completed: success. Completed with errors: warning. Failed: danger. Cancelled: neutral. Always icon plus text |
| Toast and snackbar      | One line, optional action, auto-dismiss after 5 seconds, errors stay until dismissed                                                                                                                                                   |
| Dialog and bottom sheet | Dialog on desktop, bottom sheet on Android. Title, content, at most two buttons, destructive action on the left on desktop                                                                                                             |
| QR card                 | Always black on white with a 4-module quiet zone, in both themes, so scanners work; countdown ring around the card                                                                                                                     |
| Fingerprint chip        | Monospace, grouped in blocks of 4 characters, Copy button; short form shows the first 8 characters                                                                                                                                     |
| Banner                  | Full-width strip for network problems with one action                                                                                                                                                                                  |
| Empty state             | Simple line illustration, one sentence, one primary action                                                                                                                                                                             |

### 11.4 Layout rules

- **Windows:** page padding 24 px, content max width 960 px, device cards in a responsive grid with a minimum card width of 240 px, sidebar collapses below 1000 px window width.

- **Android:** 16 dp margins, single column on compact width, two-pane (list and detail) on medium and expanded widths, navigation rail on expanded.

- Primary action stays in the same place across screens: top right on desktop, bottom right (floating or bar) on Android.

### 11.5 Wireframes (low fidelity)

**Windows, Home**

+---------+--------------------------------------------------------------+  
\| LANVault\| Home \[ Wi-Fi: HomeNet 192.168.1.8 \] \|  
\| \|--------------------------------------------------------------\|  
\| \> Home \| This device: OFFICE-PC (Windows) \[ Everyone nearby v \] \|  
\| Send \| \|  
\| Receive Nearby devices \[Rescan\] \[By address\] \|  
\| Transfers +-----------------+ +-----------------+ \|  
\| Devices \| Phone \| \| Tablet \| \|  
\| \| \| Pixel 8 (o) \| \| Tab S9 (o) \| \|  
\| \| \| Paired \| \| Not paired \| \|  
\| \| \| \[ Send \] \| \| \[ Send \] \| \|  
\| Settings\| +-----------------+ +-----------------+ \|  
\| \| Recent transfers \|  
\| Visible \| v Photos (128 files) Pixel 8 2.1 GB Completed \|  
+---------+--------------------------------------------------------------+

**Windows, Transfer detail**

+--------------------------------------------------------------------+  
\| \< Transfers Sending to Pixel 8 \[Pause\] \[Cancel\]\|  
\|--------------------------------------------------------------------\|  
\| Holiday videos (12 files) Transferring \|  
\| \[=========================-------------\] 62% 7.4 GB of 12 GB \|  
\| Speed 38.2 MB/s Avg 36.9 Peak 44.1 ETA 2 min 04 s \|  
\| ^ speed graph, last 60 seconds ................................ \|  
\| Streams 4 Chunks 1,894 of 3,052 Retries 0 \|  
\|--------------------------------------------------------------------\|  
\| Files \|  
\| v clip-001.mp4 1.2 GB Verified \|  
\| \>\> clip-007.mp4 2.4 GB \[=========------\] 58% \|  
\| clip-008.mp4 1.8 GB Waiting \|  
\| Event log (collapsed) \|  
+--------------------------------------------------------------------+

**Android, Home**

+---------------------------------+  
\| LANVault (Wi-Fi: HomeNet) \*\|  
\|---------------------------------\|  
\| Nearby devices \[Rescan\] \|  
\| +-----------------------------+ \|  
\| \| OFFICE-PC (Windows) (o) \| \|  
\| \| Paired \| \|  
\| \| \[ Send \] \| \|  
\| +-----------------------------+ \|  
\| Recent transfers \|  
\| v Report.pdf OFFICE-PC Done \|  
\| \|  
\| ( Send + ) \|  
\|---------------------------------\|  
\| Home Send Receive Transf. Dev \|  
+---------------------------------+

### 11.6 Content style

| Rule                               | Example                                                                                       |
|------------------------------------|-----------------------------------------------------------------------------------------------|
| Plain language, no jargon          | Use Not reachable, not Connection refused or ECONNRESET                                       |
| Say what happened, then what to do | Pixel 8 did not answer. Check that LANVault is open on it and both devices use the same Wi-Fi |
| Never blame the user               | Say That code has expired, not You waited too long                                            |
| Short labels                       | Send, Accept, Decline, Pause, Resume, Cancel, Pair                                            |
| Technical detail on request        | A Details link reveals error codes and addresses                                              |

## 12. Key user flows

**UF-01: First run and pairing (Windows and Android)**

1.  User installs and opens LANVault on both devices; each runs onboarding (name, permissions, save folder).

2.  On Windows, the app opens Devices with Show my QR ready. On Android, the user taps Scan QR and allows the camera.

3.  Android scans the QR. It connects to the PC address in the QR and checks the PC certificate fingerprint against the one in the QR.

4.  Android sends the one-time token. Windows validates it and stores the Android fingerprint.

5.  Both apps show Paired with \<name\>. The QR is invalidated.

6.  Failure paths: expired token (offer a new QR), fingerprint mismatch (abort with a warning that someone may be intercepting), unreachable address (open Diagnostics).

**UF-02: Send files from Windows to a paired Android phone**

1.  User drags files onto the Windows window, or opens Send and picks files.

2.  User picks the phone from the device list and clicks Send.

3.  Phone shows the incoming request (notification and sheet). User taps Accept.

4.  Transfer starts; both devices show live progress, speed and ETA.

5.  Files are verified, renamed from temporary names, and both devices notify completion.

6.  Failure paths: declined, not answered in time, phone app not open, low space on the phone.

**UF-03: Send from the Android share sheet to Windows**

1.  In any app, the user taps Share and chooses LANVault.

2.  The Send tab opens with items preselected; the user picks the PC.

3.  The PC raises a toast with Accept and Decline. The user accepts.

4.  Transfer proceeds; the phone shows a foreground-service notification.

**UF-04: Send a large folder with thousands of files**

1.  User adds a folder. The app scans in the background showing live counts.

2.  Warnings appear for unreadable files (Skip or Retry) and for more than 100,000 items.

3.  Manifest streams to the receiver, which validates paths and checks space and filesystem limits before asking the user.

4.  Files pipeline without per-file round trips. The receipt lists any renamed files.

**UF-05: Connection drops mid-transfer and recovers**

1.  Wi-Fi drops or a device sleeps. Both sides detect a dead peer within DEAD_PEER_TIMEOUT.

2.  States change to Reconnecting with the attempt number; the receiver keeps its chunk bitmap.

3.  The sender reconnects with backoff, repeats the handshake with the same transfer ID, and receives the list of chunks already held.

4.  Only missing chunks are sent. The event log reads Resumed, N chunks skipped.

5.  If reconnecting continues for 10 minutes, the state becomes Interrupted and waits for the user.

**UF-06: Sender app restarts or phone reboots**

1.  On launch, the app finds unfinished transfers in its database and lists them as Interrupted.

2.  The user taps Resume. If the source files are unchanged and still readable, the transfer continues from the receiver's bitmap.

3.  If a file changed or access was lost, the app explains it and either starts a new transfer for the changed files or asks the user to re-select them.

**UF-07: Receiver declines or does not answer**

1.  The sender sees Waiting for acceptance with a Cancel button.

2.  On Decline, the sender sees Pixel 8 declined the transfer. On timeout, Not answered.

3.  No partial data exists on either side.

**UF-08: Send to an unpaired device (one-time)**

1.  User picks a discovered but unpaired device and clicks Send. The receiver must have Allow unpaired senders on.

2.  The receiver dialog shows a warning and the sender's short fingerprint. The sender's screen shows the same short fingerprint to compare.

3.  Accept sends once. Accept and trust adds the device to the trust store.

4.  If the receiver does not allow unpaired senders, the sender is told to pair first and offered Pair now.

**UF-09: Devices cannot find each other**

1.  Home shows the empty state after 10 seconds.

2.  User opens Diagnostics. Checks show the PC and phone on the same subnet but the peer unreachable.

3.  The app says this network probably isolates devices and suggests turning on a hotspot on either device.

4.  After joining the hotspot, discovery restarts automatically and the device appears.

**UF-10: Not enough space, or FAT32 destination**

1.  Before the receiver asks the user, it checks free space and the destination filesystem.

2.  If it cannot fit, the sender is told Not enough space on the receiver, with the amounts. For FAT32, the message names the file that exceeds 4 GiB.

3.  Free space dropping during a transfer pauses it and shows Low disk space.

**UF-11: Join a hotspot by QR (P1)**

1.  One device turns on its hotspot and shows a QR with the network name and password.

2.  The other device scans it. Android asks the user to confirm joining; Windows joins using its network tools after the user approves.

3.  Once on the same subnet, discovery runs and the normal flow starts.

## 13. Error and message catalogue

Every error has a code, a plain-language message, and a next action. Codes match PROTOCOL.md section 12; local-only conditions use the same table.

| Situation                     | Code                  | Message shown                                                                              | Action offered             |
|-------------------------------|-----------------------|--------------------------------------------------------------------------------------------|----------------------------|
| Major version mismatch        | UNSUPPORTED_VERSION   | The other device runs a different version of LANVault that cannot talk to this one         | Update both apps           |
| Peer not trusted              | NOT_PAIRED            | This device is not paired with that one                                                    | Pair now                   |
| User declined                 | REJECTED              | \<Name\> declined the transfer                                                             | Close                      |
| No answer                     | TIMEOUT (offer)       | \<Name\> did not answer                                                                    | Try again                  |
| Not enough space              | DISK_FULL             | \<Name\> does not have enough free space: needs \<X\>, has \<Y\>                           | Free up space, Retry       |
| FAT32 limit                   | FILE_TOO_LARGE_FOR_FS | \<file\> is larger than 4 GiB and the destination is formatted as FAT32                    | Choose another save folder |
| Unsafe or invalid path        | PATH_INVALID          | Some file names are not valid. The transfer was not started                                | Details                    |
| Chunk corrupted               | HASH_MISMATCH         | A piece of \<file\> arrived damaged and was sent again                                     | None (automatic)           |
| Source file changed           | FILE_CHANGED          | \<file\> changed while sending and was skipped                                             | Retry failed files         |
| Receiver busy                 | BUSY                  | \<Name\> is already receiving a transfer. Yours is queued                                  | Cancel                     |
| Peer vanished                 | TIMEOUT (dead peer)   | Lost connection to \<Name\>. Reconnecting                                                  | Resume now                 |
| Cancelled by peer             | CANCELLED             | \<Name\> cancelled the transfer                                                            | Close                      |
| Protocol error                | PROTOCOL_ERROR        | Something went wrong while talking to \<Name\>                                             | Copy details               |
| Pair code expired or wrong    | PAIR_TOKEN_INVALID    | That code has expired or was already used                                                  | Show a new QR              |
| Pair fingerprint mismatch     | local                 | The device you reached is not the one in the QR. Stop and try again on a network you trust | Close                      |
| No network                    | local                 | Not connected to a network. Connect to Wi-Fi or turn on a hotspot                          | Open network settings      |
| Mobile data only (Android)    | local                 | LANVault needs Wi-Fi or a hotspot to find nearby devices                                   | Open Wi-Fi settings        |
| Listener could not start      | local                 | LANVault could not start listening on port \<N\>                                           | Choose another port        |
| Firewall blocking (Windows)   | local                 | Windows Firewall may be blocking incoming connections                                      | Fix firewall               |
| Storage access lost (Android) | local                 | LANVault can no longer read these files                                                    | Re-select files            |
| Service stopped by Android    | local                 | Android paused the transfer to save battery                                                | Resume                     |

## 14. Settings reference

Options use the same names on both platforms. Defaults are stored in settings-schema.json, which both apps read, so the table below and the code cannot disagree.

| Page          | Setting                             | Options                                                   | Default                   | Platform | Priority |
|---------------|-------------------------------------|-----------------------------------------------------------|---------------------------|----------|----------|
| General       | Device name                         | Text, 1 to 63 characters                                  | OS device name            | Both     | P0       |
| General       | Theme                               | System, Light, Dark                                       | System                    | Both     | P0       |
| General       | Language                            | English                                                   | English                   | Both     | P0       |
| General       | Launch at sign-in                   | On, Off                                                   | Off                       | Windows  | P0       |
| General       | Start minimized                     | On, Off                                                   | Off                       | Windows  | P0       |
| General       | Close button                        | Minimize to tray, Exit                                    | Minimize to tray          | Windows  | P0       |
| General       | Prevent sleep during transfer       | On, Off                                                   | On                        | Windows  | P0       |
| Receiving     | Save folder                         | Folder picker                                             | Downloads/LANVault        | Both     | P0       |
| Receiving     | Ask before accepting                | On (fixed in application v1.0)                            | On                        | Both     | P0       |
| Receiving     | Allow unpaired senders              | On, Off                                                   | Off                       | Both     | P0       |
| Receiving     | Visibility                          | Everyone nearby, Only while Receive is open, Hidden       | Everyone nearby           | Both     | P0       |
| Receiving     | Standby receive window              | Off, 15 minutes, 60 minutes (OS-permitted only)           | Off                       | Android  | P1       |
| Receiving     | Do not disturb                      | Off, 1 hour                                               | Off                       | Both     | P1       |
| Network       | Network interface                   | Auto, or a specific interface                             | Auto                      | Windows  | P0       |
| Network       | Network mode                        | Auto (local Wi-Fi client or own hotspot only)             | Auto                      | Android  | P0       |
| Network       | Listening port                      | Auto, or a number                                         | Auto (DEFAULT_PORT first) | Both     | P0       |
| Network       | Parallel streams                    | Auto, or 1 to DATA_STREAMS_MAX                            | Auto                      | Both     | P0       |
| Network       | UDP beacon fallback                 | On, Off                                                   | On                        | Both     | P0       |
| Network       | Speed limit                         | Off, or MB/s value                                        | Off                       | Both     | P1       |
| Display       | Speed units                         | MB/s, Mbps                                                | MB/s                      | Both     | P0       |
| Notifications | Show toasts or notifications        | On, Off                                                   | On                        | Both     | P0       |
| Notifications | Completion sound                    | On, Off                                                   | Off                       | Both     | P0       |
| History       | Keep history                        | 30 days, 90 days, Forever                                 | 90 days                   | Both     | P0       |
| History       | Clear history                       | Action with confirmation                                  | n/a                       | Both     | P0       |
| Security      | View my fingerprint                 | Read-only with Copy                                       | n/a                       | Both     | P0       |
| Security      | Trusted devices                     | Link to Devices tab                                       | n/a                       | Both     | P0       |
| Security      | Regenerate identity                 | Action: creates a new certificate and unpairs all devices | n/a                       | Both     | P0       |
| Storage       | Keep partial transfers              | 1 to PARTIAL_RETENTION_DAYS days                          | Default retention         | Both     | P0       |
| Storage       | Clear partial data                  | Action showing size freed                                 | n/a                       | Both     | P0       |
| Battery       | Battery help                        | Explanation and link to system battery settings           | n/a                       | Android  | P0       |
| Diagnostics   | Run diagnostics                     | Opens Diagnostics                                         | n/a                       | Both     | P0       |
| Diagnostics   | Log level                           | Errors, Info, Debug                                       | Info                      | Both     | P0       |
| Diagnostics   | Export diagnostic report            | Action; file names excluded unless opted in               | n/a                       | Both     | P0       |
| About         | Version, protocol version, licenses | Read-only                                                 | n/a                       | Both     | P0       |
| About         | Offline statement                   | LANVault never connects to the internet                   | n/a                       | Both     | P0       |
| About         | Replay onboarding                   | Action                                                    | n/a                       | Both     | P0       |

## 15. Tech stack

| Layer                   | Choice                                                                                                            | Notes                                                                                                                                                                                                             |
|-------------------------|-------------------------------------------------------------------------------------------------------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| Desktop shell           | Wails v2                                                                                                          | Go backend with a WebView2 frontend; produces a single Windows executable and installer. Wails v3 is evaluated later                                                                                              |
| Desktop language        | Go 1.27.x preferred; 1.26.x supported                                                                             | Pin a currently supported, security-patched Go toolchain in go.mod/CI; Go 1.23 is no longer an acceptable release baseline. Use standard-library crypto/tls, net, crypto/sha256 and log/slog where appropriate.   |
| Discovery               | A maintained Go mDNS library (evaluate grandcat/zeroconf and its maintained forks) plus a hand-written UDP beacon | Library choice is confirmed in milestone M2                                                                                                                                                                       |
| Local database          | SQLite through a pure-Go driver (no CGO)                                                                          | Trust store, transfers, history, settings, chunk bitmaps; WAL mode                                                                                                                                                |
| Frontend                | React 18, TypeScript (strict), Vite                                                                               | Generated Wails bindings give typed calls to Go                                                                                                                                                                   |
| Frontend state          | Zustand stores                                                                                                    | One store each for devices, transfers, settings, UI                                                                                                                                                               |
| Frontend styling        | Tailwind CSS driven by CSS variables generated from design-tokens.json                                            | Radix UI primitives for accessible dialogs, menus and tabs                                                                                                                                                        |
| Frontend extras         | lucide-react icons, react-window (virtual lists), qrcode (QR rendering), i18next (strings)                        | Framer Motion optional, with reduced-motion support                                                                                                                                                               |
| Frontend testing        | Vitest, Testing Library, ESLint, Prettier                                                                         | Storybook optional for components                                                                                                                                                                                 |
| Android language and UI | Kotlin 2.x, Jetpack Compose, Material 3, Navigation Compose                                                       | minSdk 29; pin a release-tested targetSdk and compileSdk. When targeting API 37+, implement Android 17 local-network runtime permission; re-check Play policy and manifest rules at release.                      |
| Android architecture    | MVVM with unidirectional data flow, Hilt for dependency injection, Coroutines and Flow                            |                                                                                                                                                                                                                   |
| Android storage         | Room (transfers, trusted devices), DataStore (settings), SAF and MediaStore (files)                               |                                                                                                                                                                                                                   |
| Android crypto          | Platform TLS 1.3; BouncyCastle bcpkix for X.509 generation; Android Keystore for the private key where possible   |                                                                                                                                                                                                                   |
| Android QR              | CameraX with ZXing core                                                                                           | No Google Play services dependency, so it also works on devices without them                                                                                                                                      |
| Android serialization   | kotlinx.serialization (JSON)                                                                                      | Matches PROTOCOL.md messages                                                                                                                                                                                      |
| Android testing         | JUnit, MockK, Turbine, Compose UI tests                                                                           | ktlint and Detekt for style                                                                                                                                                                                       |
| Shared contract         | PROTOCOL.md, protocol-constants.json, design-tokens.json, settings-schema.json, testvectors/                      | Mandatory before wire implementation: PROTOCOL.md, versioned constants, tokens, settings schema and shared positive/negative test vectors checked into source control. CI fails on missing or drifting artifacts. |
| CI                      | GitHub Actions                                                                                                    | Go vet and staticcheck, race detector, fuzz seeds, ESLint, tsc, Vitest, Android lint and unit tests, generated-code drift check, contrast check, test-vector checks on both platforms                             |

## 16. Architecture

### 16.1 System context

Windows PC Android phone  
+--------------------------+ +---------------------------+  
\| React + TypeScript UI \| \| Compose UI \|  
\| (WebView2) \| \| \|  
\| \| Wails bridge \| \| \| ViewModels \|  
\| Go App layer (bindings) \| \| Domain / use cases \|  
\| \| \| \| \| \|  
\| Go core library \|\<== mutual TLS 1.3 =\>\| Protocol + transfer engine\|  
\| discovery, pairing, \| over the local \| (Kotlin, JVM module) \|  
\| protocol, transfer, \| network (Wi-Fi, \| Foreground service \|  
\| store, fsutil \| hotspot) \| Room, DataStore, SAF \|  
+--------------------------+ +---------------------------+  
^ mDNS and UDP beacon (discovery) ^  
+-------------------------------------+

No component talks to anything outside the local network.

### 16.2 Repository layout

lanvault/  
docs/ PRD.md, PROTOCOL.md, protocol-constants.json,  
design-tokens.json, settings-schema.json  
testvectors/ shared fixtures asserted by Go and Kotlin tests  
core/ Go library (no UI dependencies)  
identity/ discovery/ pairing/ protocol/ transfer/  
fsutil/ store/ netutil/ diag/  
cli/ Go developer CLI (send, receive) used for early testing  
desktop/ Wails app: app.go, bindings, frontend/ (React + TS)  
android/ Gradle project: app, protocol, transfer, data, ui modules  
tools/ code generators (constants, tokens) and CI checks

### 16.3 Go core modules

| Package   | Responsibility                                                                                                               |
|-----------|------------------------------------------------------------------------------------------------------------------------------|
| identity  | Generate and load the ECDSA P-256 key and certificate, compute fingerprint and device_id, protect the key (DPAPI on Windows) |
| netutil   | Enumerate eligible interfaces and addresses, filter virtual and link-local adapters, watch for network changes               |
| discovery | mDNS advertise and browse, UDP beacon send and receive, merge by device_id, expiry                                           |
| pairing   | Create and validate one-time QR tokens, run the scanner and displayer sides, maintain the trust store                        |
| protocol  | Frame codec, message structs, version negotiation, limits; fuzzed                                                            |
| transfer  | Manifest builder, sender and receiver state machines, chunk scheduler, bitmaps, resume logic, speed and ETA                  |
| fsutil    | Path validation and sanitization, collision handling, free-space and filesystem-type checks, extended-length path handling   |
| store     | SQLite schema, migrations, queries                                                                                           |
| diag      | Diagnostics checks (network, listener, firewall, discovery, reachability)                                                    |

The core exposes a small Go API (start, stop, subscribe to events, start transfer, respond to request) and has no knowledge of Wails or the UI, so the same code runs in the CLI, in tests, and in the desktop app.

### 16.4 Transfer engine design

**Sender pipeline.** Manifest builder, then a scheduler that assigns the next needed chunk to the data stream with window space. Per stream: reader reads a chunk from the file, computes its SHA-256, writes one DATA frame. Chunk bytes are not retained after the write completes; if a retransmit is needed, the chunk is read again from the file.

**Receiver pipeline.** Per stream: frame reader reads one DATA frame into a pooled buffer, verifies the hash, hands the chunk to the disk writer, which writes at the correct offset in the temporary file. A durability stage fsyncs written files about once per second and then flips the chunk bits in the persisted bitmap; only then are CHUNK_ACKs released. This rule means a power loss can never leave a bit set for data that is not on disk.

**Window sizing.** Acks can lag by about a second because of batched fsync. The default window (WINDOW_CHUNKS_PER_STREAM across DATA_STREAMS_DEFAULT streams) must cover that lag at the target link speed; a CI benchmark checks that the sender never stalls waiting for acks on a link of 100 MB/s.

**Memory budget.** Buffers are pooled. Transfer buffers stay within 64 MiB on desktop and 32 MiB on Android. On devices with little RAM, the sender proposes a smaller chunk size within the allowed range.

**Manifest at scale.** The manifest is stored in SQLite and streamed in parts; the manifest hash is computed incrementally while entries are sent and received, so memory use does not grow with the file count.

**State machines.** Both sides use explicit states that match the UI pills: Idle, Offered, Awaiting decision, Accepted, Transferring, Paused, Reconnecting, Verifying, Finishing, Completed, CompletedWithErrors, Failed, Cancelled, Interrupted. Every transition is persisted and logged.

### 16.5 Wails bridge

Bound Go methods (called from TypeScript, typed bindings generated by Wails):

| Group     | Methods                                                                                                             |
|-----------|---------------------------------------------------------------------------------------------------------------------|
| State     | GetAppState, GetSettings, UpdateSettings                                                                            |
| Devices   | ListNearbyDevices, Rescan, ListTrustedDevices, RenameDevice, RemoveDevice, SetAutoAccept, ConnectByAddress          |
| Pairing   | GetPairingQR, RefreshPairingQR, CancelPairing                                                                       |
| Sending   | PickFiles, PickFolder, ScanItems, StartSend, RetryFailed                                                            |
| Transfers | ListTransfers, GetTransfer, PauseTransfer, ResumeTransfer, CancelTransfer, RespondToRequest                         |
| System    | PickSaveFolder, OpenPath, ShowInFolder, RunDiagnostics, FixFirewall, ExportReport, ClearHistory, RegenerateIdentity |

Events (Go to frontend through the Wails runtime event bus):

| Event                             | Payload                                                | Rate                              |
|-----------------------------------|--------------------------------------------------------|-----------------------------------|
| devices:updated                   | Full nearby list                                       | On change, debounced 200 ms       |
| network:changed                   | Interfaces and addresses                               | On change                         |
| request:incoming                  | Request summary and warnings                           | Once per request                  |
| transfer:state                    | Transfer id and new state                              | On change                         |
| transfer:progress                 | Bytes, speed, ETA, per-file progress for visible files | At most 4 per second per transfer |
| pairing:completed, pairing:failed | Device or cause                                        | Once                              |
| settings:changed                  | Changed keys                                           | On change                         |
| toast                             | Level, message, optional action                        | On demand                         |
| files:dropped                     | Absolute paths from native drag and drop               | On drop                           |
| diagnostics:progress              | Check id and result                                    | During a run                      |

The frontend never computes transfer statistics; it displays what the core sends, so desktop and Android definitions of speed and ETA cannot diverge.

### 16.6 Frontend architecture

- Feature folders: home, send, receive, transfers, devices, settings, diagnostics, onboarding; shared components and hooks; generated bindings.

- Stores hold normalized data keyed by id; selectors derive view models; no business logic in components.

- Lists with many rows (files in a transfer, history) are virtualized.

- Theme switch sets a data attribute on the root element; all colors come from CSS variables.

- Strict TypeScript, no implicit any, ESLint accessibility rules enabled.

### 16.7 Android architecture

| Module   | Content                                                                                                                                       |
|----------|-----------------------------------------------------------------------------------------------------------------------------------------------|
| app      | Application, DI wiring, manifest, navigation, share-target activity, notification channels                                                    |
| ui       | Compose screens, components, theme generated from tokens, ViewModels                                                                          |
| protocol | Pure Kotlin (JVM) frame codec, messages, constants generated from JSON, test vectors; no Android dependencies, so it runs in plain unit tests |
| transfer | Sender and receiver engines, scheduler, bitmaps, speed and ETA; depends only on protocol and abstract file and database interfaces            |
| data     | Room database, DataStore, SAF and MediaStore file access, keystore and certificate handling                                                   |

The transfer engine and persisted Room state are independent of Activity/UI lifetime. A foreground service owns active work only when permitted by user action and the OS; it is not an indefinite background listener. ConnectivityManager selects the actual local interface for Wi-Fi-client, phone-hotspot-host and Windows-hotspot-client scenarios; bind outgoing sockets on that network without cellular fallback. Discovery uses NsdManager plus the UDP beacon where LAN permission permits it. For Android 17 targetSdk 37+, permission state gates all direct LAN I/O, including recovery from denial/revocation.

### 16.8 Error handling and logging

- Errors are typed values carrying a code from PROTOCOL.md section 12 or a local code; the UI maps codes to the catalogue in Section 13.

- Structured logs with levels (errors, info, debug), rotated, 5 files of 1 MB. Logs never contain file contents; file names are logged only at debug level.

- Panics in the Go core are recovered at goroutine boundaries, logged, and turn the affected transfer into Failed rather than crashing the app.

### 16.9 AI-agent development acceptance contract

For every milestone, the coding agent MUST (a) implement actual Go/Kotlin network and filesystem code, not simulated transfers; (b) provide cross-platform protocol fixtures, error-handling and concurrency tests; (c) run build, race/fuzz checks where supported, transfer and crash-resume integration checks; (d) report measured results and untested hardware assumptions; and (e) preserve working milestones. The next milestone MUST NOT start until its predecessor passes. UI mock data may exist only behind explicit development flags and must never represent real peers or completed transfers in release builds.

## 17. Data model

The same logical schema is used by SQLite on desktop and Room on Android.

| Table                                           | Columns (main)                                                                                                                                                                                         | Purpose                                          |
|-------------------------------------------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|--------------------------------------------------|
| trusted_devices                                 | device_id (primary key), fingerprint, name, nickname, platform, paired_at, last_seen_at, last_address, auto_accept                                                                                     | Trust store                                      |
| transfers                                       | transfer_id (primary key), direction, peer_device_id, peer_name, state, chunk_size, total_bytes, file_count, dir_count, manifest_hash, bytes_done, started_at, ended_at, error_code, destination_label | One row per transfer                             |
| transfer_entries                                | transfer_id, entry_index, type, path, size, mtime_ms, state, bytes_done, error_code                                                                                                                    | Manifest and per-file state; streamed in and out |
| chunk_bitmaps                                   | transfer_id, entry_index, bitmap (blob), updated_at                                                                                                                                                    | Resume state                                     |
| transfer_events                                 | transfer_id, at, kind, message                                                                                                                                                                         | Event log shown in detail view                   |
| settings                                        | key, value                                                                                                                                                                                             | Matches settings-schema.json                     |
| identity (desktop file, Android keystore alias) | certificate, protected private key                                                                                                                                                                     | Device identity                                  |

Data locations: Windows uses the per-user application data folder for the database, logs and identity; Android uses app-private storage. Partial file data lives next to the destination file under a temporary name; resume state lives in the database.

## 18. Security and privacy requirements

### 18.1 Threat model

| Threat                                                              | Mitigation                                                                                                                                                                                                                                      |
|---------------------------------------------------------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| Eavesdropping on the local network                                  | TLS 1.3 with authenticated encryption on every connection, including data streams                                                                                                                                                               |
| Man-in-the-middle during pairing                                    | Fingerprint carried out-of-band in the QR; one-time token authenticates the scanner                                                                                                                                                             |
| Rogue device impersonating a trusted one (spoofed mDNS name)        | Identity is the certificate fingerprint, not the name; trust store pins fingerprints                                                                                                                                                            |
| Unpaired stranger sends files                                       | Unpaired senders are off by default; explicit accept always; short fingerprint shown                                                                                                                                                            |
| Malicious paths (traversal, reserved names, absolute paths)         | Canonicalize and validate each manifest path and its actual parent immediately before write; reject traversal, absolute paths, special devices, junctions/symlinks and target escapes, including race attempts; never overwrite existing files. |
| Resource exhaustion (huge manifest, many connections, giant frames) | Frame and entry caps, limits on concurrent connections and per-address rate, one active transfer, bounded buffers                                                                                                                               |
| Replay of a pairing QR                                              | Single-use token, short lifetime, invalidated after 3 failures                                                                                                                                                                                  |
| TLS downgrade                                                       | TLS 1.3 only, no fallback                                                                                                                                                                                                                       |
| Stolen or lost device                                               | Private key in keystore or DPAPI-protected file; user can remove the device on the other side; Regenerate identity                                                                                                                              |
| Malicious files                                                     | Never auto-open or execute; user opens files explicitly; OS security handles scanning                                                                                                                                                           |
| Local malware reading the trust store                               | Out of scope beyond OS file permissions and key protection                                                                                                                                                                                      |
| Plaintext files on endpoints                                        | Out of scope; stated clearly in About                                                                                                                                                                                                           |

### 18.2 Requirements

| ID        | Requirement                                                                                                                                                                                                                                                                                           | Priority |
|-----------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|----------|
| FR-SEC-01 | Use TLS 1.3 with pinned fingerprints for trusted-device transfer channels; the initial pairing channel MUST authenticate the peer identity against the out-of-band QR fingerprint and one-time token before trust is persisted. Fail closed on mismatch; do not bypass validation for unpaired peers. | P0       |
| FR-SEC-02 | Private keys never leave the device and are never logged                                                                                                                                                                                                                                              | P0       |
| FR-SEC-03 | Compare fingerprints in constant time where practical; compare pairing tokens in constant time                                                                                                                                                                                                        | P0       |
| FR-SEC-04 | Rate-limit failed pairing and handshake attempts per source address                                                                                                                                                                                                                                   | P0       |
| FR-SEC-05 | Parse every peer-supplied length against negotiated limits before allocating                                                                                                                                                                                                                          | P0       |
| FR-SEC-06 | Fuzz tests on the frame codec and manifest parser in both languages                                                                                                                                                                                                                                   | P0       |
| FR-SEC-07 | Receiver never writes outside the chosen destination; symlinks in the destination path are not followed for writes                                                                                                                                                                                    | P0       |
| FR-SEC-08 | Regenerate identity requires confirmation and clears the trust store                                                                                                                                                                                                                                  | P0       |
| FR-SEC-09 | Security review of the pairing flow and TLS configuration before v1.0 release, ideally by someone who did not write it                                                                                                                                                                                | P0       |
| FR-SEC-10 | Before each file creation, validate canonical destination confinement with OS-safe no-follow semantics; reject traversal, symlink/junction races, reserved names, device files and external absolute paths. No write occurs outside the user-selected destination.                                    | P0       |
| FR-SEC-11 | Bind resume state to authenticated peer identity, transfer ID, manifest hash, file metadata and verified chunk hashes. Reject replay, mismatched peers, tampered databases/bitmaps and inconsistent source revisions; never mark unverified bytes completed.                                          | P0       |
| FR-SEC-12 | Only persist trust after both-party identity verification and explicit user consent; reject expired/replayed QR tokens, spoofed mDNS identities and TLS downgrade. Test certificates, errors and token-rate limits before public release.                                                             | P0       |

### 18.2.1 Security release gates (P0)

A release candidate MUST pass automated adversarial suites on Go and Kotlin peers: (1) QR replay, expired token, wrong fingerprint, rogue discovery and unpaired transfer attempts are rejected; (2) path traversal, symlink/junction races and invalid manifests never write outside the selected destination; (3) malformed frames and excessive lengths remain bounded; (4) tampered resume bitmaps, modified source files and wrong peers cannot cause false completion; (5) each successful transfer is verified against the original file hash. Record command, fixture, expected outcome and actual outcome; security review cannot be replaced with UI screenshots. (5) Windows file/directory aliases such as file A vs directory a or file A vs file a/x MUST reject before any write; file-collision suffixes cannot overwrite future directory names. (6) MANIFEST_PART batching MUST satisfy both MANIFEST_PART_MAX_ENTRIES and the actual encoded MAX_CONTROL_FRAME byte length; oversized single entries and pre-allocation oversized lengths fail safely.

### 18.3 Privacy requirements

- No telemetry, analytics, advertising identifiers, accounts, or remote configuration.

- The app opens no connection to a non-local address. A release check captures traffic during a full test session and fails the release if any non-local destination appears (metric M7).

- The app does not auto-update. Users get new versions by downloading them; this keeps the offline promise intact.

- Diagnostic reports exclude file names and contents unless the user opts in.

## 19. Non-functional requirements

| ID     | Area              | Requirement                                              | Target                                                                     |
|--------|-------------------|----------------------------------------------------------|----------------------------------------------------------------------------|
| NFR-01 | Throughput        | Transfer speed relative to raw TCP on the same link      | At least 85 percent                                                        |
| NFR-02 | CPU               | Desktop at sustained 40 MB/s on a reference mid-range PC | At most 35 percent of one core                                             |
| NFR-03 | Memory            | Transfer buffers, desktop and Android                    | 64 MiB and 32 MiB maximum                                                  |
| NFR-04 | Startup           | Desktop and Android                                      | 2 s and 1.5 s to interactive                                               |
| NFR-05 | Discovery         | Time to first peer on home Wi-Fi                         | 3 s median                                                                 |
| NFR-06 | UI responsiveness | UI thread never blocked during transfers; frame rate     | No block over 100 ms; at least 55 fps on the reference PC                  |
| NFR-07 | Scale             | Items in one transfer                                    | 100,000 comfortable; 1,000,000 best effort with bounded memory             |
| NFR-08 | Reliability       | Power loss or process kill during transfer               | No corrupted completed files; resume works from the last persisted chunk   |
| NFR-09 | Reliability       | Reconnect after a network drop                           | Back to Transferring within 10 s of the link returning                     |
| NFR-10 | Battery (Android) | Background activity when not transferring or receiving   | None; no wake-ups                                                          |
| NFR-11 | Size              | Windows installer and Android APK                        | About 40 MB and 15 MB maximum                                              |
| NFR-12 | Compatibility     | OS versions                                              | Windows 10 21H2 or later; Android 10 or later                              |
| NFR-13 | Accessibility     | Standard                                                 | WCAG 2.2 AA (Section 10.6)                                                 |
| NFR-14 | Maintainability   | Test coverage                                            | Protocol codecs 100 percent of branches; core packages at least 70 percent |
| NFR-15 | Privacy           | Non-local connections                                    | Zero                                                                       |

## 20. Quality and testing strategy

| Layer                              | What is tested                                                                                                                                                                                                                                  | How                                                                                                                                                                                                                                                    |
|------------------------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| Unit                               | Frame codec, bitmaps, manifest hash, path sanitization, speed and ETA math, size formatters, state machines                                                                                                                                     | Go test and JUnit; shared test vectors from testvectors/ asserted in both languages                                                                                                                                                                    |
| Fuzz                               | Frame parser, manifest parser, path validator                                                                                                                                                                                                   | Go native fuzzing and a Kotlin fuzz harness; seeds from test vectors                                                                                                                                                                                   |
| Integration                        | Go to Go, Kotlin to Kotlin, and Go to Kotlin full transfers in both directions                                                                                                                                                                  | Go-to-Go automated from M1/M2; Go-to-Kotlin and Kotlin-to-Go automated from M4 onward; Kotlin-to-Kotlin after both Android directions exist. Assert shared protocol test vectors including negative cases; run on a loopback and two physical devices. |
| Chaos                              | Connection reset, packet loss and latency, Wi-Fi toggle, device sleep, app kill, kill during write, disk full, source file modified, receiver name collision                                                                                    | Network shaping tools plus scripted process kills                                                                                                                                                                                                      |
| Filesystem edge cases              | Unicode names, very long paths, reserved Windows names, zero-byte files, sparse files, 50 GB file, 1,000,000 small files, FAT32 and exFAT destinations                                                                                          | Automated fixtures on real volumes                                                                                                                                                                                                                     |
| Network lab                        | Home Wi-Fi with and without WAN; Android hotspot (mobile data OFF); Windows hotspot (phone mobile data OFF); Ethernet plus Wi-Fi; multicast-blocked network; client isolation; VPN and firewall edge cases; explicit interface-switch tests.    | Manual matrix with a recorded pass or fail per cell                                                                                                                                                                                                    |
| Device matrix                      | Windows 10/11 x64, adapter variants; Android 10–17 where test hardware/emulators are available, with special targetSdk 35/36/37 tests. Validate runtime permission denial, revocation and background dataSync limits.                           | Manual and scripted                                                                                                                                                                                                                                    |
| Performance                        | Throughput, CPU, memory, discovery time, startup                                                                                                                                                                                                | Benchmark suite compared with a plain TCP baseline                                                                                                                                                                                                     |
| Security                           | Mutual TLS pairing, rogue peers, token replay/expiry, permission bypass, malicious manifests/paths, symlink races, unexpected filesystem writes, resource exhaustion, corrupted/replayed resume state, log redaction and negative test vectors. | Automated P0 adversarial tests in both languages and independent security review. Assert failed attacks do not create files, elevate trust or report Verified; preserve reproducible test evidence (FR-SEC-09 to FR-SEC-12).                           |
| Android lifecycle and permission   | Target SDK 35/36/37, runtime LAN permission grant/denial/revocation, Android 16 opt-in, foreground-service timeout and restart, hotspot-host and station role, cellular data off.                                                               | Automated tests where possible plus physical Android 15–17 device runs; verify persisted interruption state, clear user-facing errors, no unauthorized WAN fallback.                                                                                   |
| Offline installation and first-run | Clean Windows VM with no WebView2 and internet physically unavailable; install, launch, pair and transfer. No DNS/WAN requests after installation.                                                                                              | Use bundled Evergreen Standalone Runtime; capture traffic. Both installer and app must function fully offline.                                                                                                                                         |
| Privacy                            | Packet capture of a full session                                                                                                                                                                                                                | Release gate for M7                                                                                                                                                                                                                                    |
| Usability                          | Moderated sessions with new users, including a non-technical participant                                                                                                                                                                        | At least 10 sessions before release                                                                                                                                                                                                                    |
| Accessibility                      | Keyboard-only run, Narrator, TalkBack, 200 percent font scale, contrast checks                                                                                                                                                                  | Manual plus automated                                                                                                                                                                                                                                  |

## 21. Build, packaging and release

| Item            | Specification                                                                                                                                                                                                                                                          |
|-----------------|------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| Windows build   | wails build producing the executable, wrapped in an NSIS installer                                                                                                                                                                                                     |
| WebView2        | If WebView2 is absent, the offline distribution packages Microsoft Evergreen Standalone Runtime (correct architecture) or a supported fixed runtime; do not rely solely on the network-downloading Evergreen Bootstrapper. Clean-VM offline-install test is mandatory. |
| Firewall        | Installer adds a program-based inbound rule for the installed executable on the Private profile (covers TCP listening and UDP discovery). The in-app Fix firewall button repeats this with an elevation prompt                                                         |
| Code signing    | Sign the executable and installer with a code-signing certificate before public release; until then document the SmartScreen warning                                                                                                                                   |
| Updates         | No auto-update. The About screen shows the version; users download new releases manually                                                                                                                                                                               |
| Android build   | Gradle with R8 shrinking; signed release APK, and an App Bundle if Play distribution is used                                                                                                                                                                           |
| Distribution    | GitHub Releases with published checksums; other channels per open question OQ-2                                                                                                                                                                                        |
| Versioning      | Semantic versions for each app; the protocol has its own major and minor version, and an app declares the range it supports                                                                                                                                            |
| Reproducibility | Pinned toolchain and dependency versions; generated files (constants, tokens, settings defaults) are checked into CI as a drift check                                                                                                                                  |
| Licenses        | Third-party license list is generated at build time and shown in About                                                                                                                                                                                                 |

## 22. Milestones

Estimates assume one full-time developer; part-time work scales them proportionally. P1 features are cut first if the schedule slips.

| Milestone                | Duration | Deliverables                                                                                                                                                                                          | Exit criteria                                                                                                                                                                                                                                                              |
|--------------------------|----------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| M0 Specs                 | 1 week   | This PRD; checked-in PROTOCOL.md, protocol-constants.json, design-tokens.json, settings-schema.json; Go/Kotlin shared and negative testvectors; version pinning; Android permission/lifecycle matrix. | All companion artifacts included, version-aligned and reviewed; security testvectors cover cross-platform file/directory aliases and manifest frame byte caps; validation script passes with --prd. Go/Kotlin minimal fixtures read the same vectors. No guessed defaults. |
| M1 Go core and CLI       | 3 weeks  | Framing, TLS with pinning, manifest, chunking, hashing, resume, send and receive CLI                                                                                                                  | Go CLI sends and receives 10 GiB Go-to-Go correctly; SHA-256 matches; simulated crashes and resume pass 100 runs. No UI-first placeholders accepted.                                                                                                                       |
| M2 Discovery and pairing | 2 weeks  | mDNS, beacon, QR pairing, trust store                                                                                                                                                                 | Devices find each other on 3 different networks; pairing test suite green                                                                                                                                                                                                  |
| M3 Windows UI            | 4 weeks  | Wails app: Home, Send, Receive, Transfers, Devices, Settings, onboarding                                                                                                                              | Real Go CLI↔Windows app transfer works on LAN with WAN disabled; pairing and file writes verified, not only screen mocks.                                                                                                                                                  |
| M4 Android receive       | 3 weeks  | Native Kotlin protocol module and Android receiver, SAF, QR scan, API 37 LAN permission, bounded user-initiated foreground lifecycle, hotspot-host routing tests.                                     | Go-to-Android transfer and resume verified on hardware with mobile data OFF, plus targetSdk permission and service-timeout denial/recovery tests.                                                                                                                          |
| M5 Android send          | 2 weeks  | Send tab, share target, sender engine, folder enumeration performance                                                                                                                                 | Android to Windows works including a 10,000-file folder                                                                                                                                                                                                                    |
| M6 Polish                | 3 weeks  | Diagnostics, tray, notifications, installer and firewall, accessibility pass, error catalogue, history                                                                                                | All P0 requirements implemented                                                                                                                                                                                                                                            |
| M7 Beta and lab          | 3 weeks  | Network/device/Android-version matrix, chaos/performance runs, malicious-peer/security suite, offline Windows clean-VM install, usability and packet capture.                                         | M1–M7 executable evidence collected; all M1–M8 goals demonstrated or release blocked/explicitly waived per Section 25.                                                                                                                                                     |
| M8 Release v1.0          | 1 week   | Signed builds, release notes, checksums, documentation                                                                                                                                                | Definition of done in Section 25 met                                                                                                                                                                                                                                       |

v1.1 follows with hotspot QR join, text sharing, Explorer context menu, speed limit, webcam QR scanning, SAS pairing and optional, explicitly enabled per-paired-device auto-accept.

## 23. Risks and mitigations

| ID  | Risk                                                                                         | Likelihood | Impact | Mitigation                                                                                                                                                                                                                                    |
|-----|----------------------------------------------------------------------------------------------|------------|--------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| R1  | Devices cannot discover each other because of client isolation or blocked multicast          | High       | High   | Beacon fallback, QR and manual address, Diagnostics that identify the cause, hotspot guidance                                                                                                                                                 |
| R2  | Android kills or limits background transfers                                                 | High       | High   | Foreground service, battery guidance, clean pause and resume, interrupted-state recovery                                                                                                                                                      |
| R3  | Foreground service time limits on recent Android versions                                    | Medium     | High   | dataSync budget is six hours per 24 hours for targetSdk \>=35 on Android 15+; avoid perpetual idle service, require user-started bounded sessions, implement onTimeout() and persisted recovery, test exhaustion and background-start denial. |
| R4  | The two implementations drift apart                                                          | Medium     | High   | Spec first, generated constants, shared test vectors, cross-platform integration from M4                                                                                                                                                      |
| R5  | Slow SAF enumeration of large folders                                                        | Medium     | Medium | Direct content-resolver queries, benchmark in M5                                                                                                                                                                                              |
| R6  | Antivirus or SmartScreen flags the Windows build                                             | Medium     | Medium | Code signing, published checksums, minimal and transparent behavior, pre-release scanning                                                                                                                                                     |
| R7  | Windows Firewall blocks first use                                                            | High       | Medium | Installer rule, Fix firewall button, Diagnostics check                                                                                                                                                                                        |
| R8  | WebView2 runtime missing                                                                     | Low        | Medium | Bundle the Evergreen Standalone Runtime for offline installs; smoke-test on a clean, disconnected Windows VM. Bootstrapper-only installers fail the offline acceptance gate.                                                                  |
| R9  | Pairing or TLS configuration flaw                                                            | Low        | High   | Security gate with reproducible negative cross-language test vectors: pinned identity, QR expiry/replay, malicious paths and resume-state tampering, plus independent review.                                                                 |
| R10 | Wi-Fi Direct proves impractical on Windows                                                   | High       | Low    | Already P2; v1 does not depend on it                                                                                                                                                                                                          |
| R11 | Scope creep delays v1                                                                        | Medium     | High   | Strict P0, P1, P2 discipline; every new feature must displace another                                                                                                                                                                         |
| R12 | Thermal throttling or battery drain on long transfers                                        | Medium     | Medium | Monitor, reduce streams adaptively, clear messaging                                                                                                                                                                                           |
| R13 | Low-end phones are CPU-bound on hashing and encryption                                       | Medium     | Medium | Hardware-accelerated primitives, smaller chunks, reduced streams; revisit hash choice if measured                                                                                                                                             |
| R14 | Android 17 local-network runtime permission denied/revoked or not declared for targetSdk 37+ | High       | High   | API-aware manifest/runtime path; human-readable denial and Settings recovery; negative tests on targetSdk 35/36/37.                                                                                                                           |
| R15 | Hotspot-host vs Wi-Fi-client routing leaks to cellular or wrong interface                    | Medium     | High   | Explicit interface selection, socket binding and offline network tests for both Android- and Windows-hosted hotspots.                                                                                                                         |
| R16 | AI-generated screens appear complete but transfer/core protocol not functional               | Medium     | High   | Require runnable CLI + reproducible integration/chaos test evidence at each milestone; never accept mock-only completion.                                                                                                                     |

## 24. Open questions

| ID   | Question                                                                                                                                                                                                                                                      | Needed by                   |
|------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|-----------------------------|
| OQ-1 | Which open-source license, if the project is published?                                                                                                                                                                                                       | M0                          |
| OQ-2 | Distribution channels: GitHub Releases only, or also Google Play and F-Droid?                                                                                                                                                                                 | M7                          |
| OQ-3 | Which code-signing certificate and who holds it?                                                                                                                                                                                                              | M7                          |
| OQ-4 | How should Windows create or join a hotspot for v1.1 (native APIs through a helper, or guided manual steps)?                                                                                                                                                  | v1.1 planning               |
| OQ-5 | Which supported Android mechanism (bounded user-initiated dataSync foreground service vs permitted user-initiated transfer job) best meets long-transfer requirements on each target SDK, given tested six-hour background allowance and restart constraints? | M5                          |
| OQ-6 | Should multiple simultaneous incoming transfers be allowed after v1?                                                                                                                                                                                          | v1.1 planning               |
| OQ-7 | Final app icon and brand identity                                                                                                                                                                                                                             | M6                          |
| OQ-8 | Keep SHA-256 for chunk hashing, or move to BLAKE3 if low-end phones are CPU-bound?                                                                                                                                                                            | M5 benchmarks               |
| OQ-9 | For Play Store distribution on targetSdk \>=37, is broad ACCESS_LOCAL_NETWORK justified over a suitable NsdManager system-mediated picker? Document the decision and declaration requirements.                                                                | M0 / before Android release |

## 25. Definition of done for v1.0

- Every P0 requirement in this document is implemented and traceable to a passing test or a recorded manual check. Each acceptance item includes a runnable command/test case, a real-device result where applicable, and a link to its evidence; compile-only and UI-only completion do not count. Specification integrity gate: run python3 tools/verify_companions.py --self-test --prd docs/PRD.md and confirm all categories and mutation tests PASS.

- Metrics M1 to M8 meet their targets, or each shortfall has a written, accepted waiver.

- Windows to Android and Android to Windows transfers work for: a single small file, a 20 GB file, a 10,000-file folder, an empty folder, and a zero-byte file, with matching hashes.

- Resume works after: connection reset, Wi-Fi toggle, device sleep, app kill on the sender, and app kill on the receiver.

- Pairing, diagnostics, firewall setup and onboarding work on clean Windows 10 and Windows 11 installations, Android 10 and 15, and Android 17/API 37 (or the nearest available pre-release-compatible test device/emulator). Explicitly test LAN permission denial/revocation and permitted service timeout/restart.

- Full-session packet capture shows no non-local connections. Test Windows offline installer plus first run on a disconnected VM without WebView2, and check both hotspot-host directions with mobile data disabled.

- Accessibility checks pass (Section 10.6).

- Signed builds, checksums and release notes are published; known limitations (Section 6.3) are listed in the README. If signing certificates are unavailable, release remains pre-release and this blocker is documented rather than declaring the public-release definition of done satisfied.

## 26. Glossary and document history

| Term                            | Meaning                                                                                                  |
|---------------------------------|----------------------------------------------------------------------------------------------------------|
| AP isolation (client isolation) | A router setting that stops Wi-Fi clients from talking to each other                                     |
| Chunk                           | A fixed-size piece of a file sent and verified independently (4 MiB by default)                          |
| Doze                            | Android power-saving mode that restricts background work                                                 |
| EMA                             | Exponential moving average, used to smooth speed                                                         |
| Fingerprint                     | SHA-256 of a device certificate, used as its identity                                                    |
| Foreground service              | An Android service with a visible notification that may keep running in the background                   |
| Manifest                        | The list of files and folders in a transfer, with sizes and dates                                        |
| mDNS                            | Multicast DNS, used to find services on a local network without a server                                 |
| mTLS                            | Mutual TLS: both sides present certificates                                                              |
| SAF                             | Android Storage Access Framework, the system file and folder picker and access layer                     |
| SAS                             | Short authentication string, a short code users compare to confirm two devices are talking to each other |
| TOFU                            | Trust on first use                                                                                       |
| WebView2                        | The Microsoft Edge based web runtime that Wails uses on Windows                                          |
| Wi-Fi Direct                    | A standard for direct device-to-device Wi-Fi links without a router                                      |

| Version | Date            | Change                                                                                                                                                                                                                                                                                                               |
|---------|-----------------|----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| 0.1     | 1 November 2026 | First full draft of the PRD, with PROTOCOL.md v1.0 draft as companion                                                                                                                                                                                                                                                |
| 1.1     | 1 November 2026 | Android 17 LAN permissions; bounded Android background services; local hotspot routing; offline Windows installer; supported Go toolchain; security and AI-agent test gates.                                                                                                                                         |
| 1.2     | 1 November 2026 | Aligned wire protocol v1.1 and release priorities; deferred per-device auto-accept and standby receive window to P1; normalized Android network mode; closed Windows file/directory collision loopholes; enforced manifest control-frame byte caps; added cross-platform negative tests and stronger PRD validation. |

### Implementation reference links (verified 1 November 2026)

[<u>Android 17 local-network permission — Android Developers</u>](https://developer.android.com/privacy-and-security/local-network-permission)

[<u>Foreground-service dataSync timeouts — Android Developers</u>](https://developer.android.com/develop/background-work/services/fgs/timeout)

[<u>Offline WebView2 Evergreen Standalone Runtime — Microsoft</u>](https://developer.microsoft.com/en-us/microsoft-edge/webview2)

[<u>Go release history and support policy — go.dev</u>](https://go.dev/doc/devel/release)

[<u>Google Play local-network permission policy — Play Console</u>](https://support.google.com/googleplay/android-developer/answer/16935362)
