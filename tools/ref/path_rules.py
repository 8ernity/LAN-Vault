"""Specification oracle for LANVault paths (PROTOCOL.md §10).

NOT production filesystem code. The receivers must additionally use safe, no-follow
filesystem APIs and atomic no-replace finalization. The oracle models Windows as
case-insensitive and POSIX as case-sensitive to enable cross-language contract tests.
"""
import re
import unicodedata
from dataclasses import dataclass

MAX_PATH_BYTES = 4096
MAX_SEGMENT_BYTES = 255
WIN_BAD = set('<>:"|?*')
BIDI = set(chr(c) for c in list(range(0x202A, 0x202F)) + list(range(0x2066, 0x206A)))
RESERVED = {"CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$"} | {f"COM{i}" for i in range(1, 10)} | {f"LPT{i}" for i in range(1, 10)}
DRIVE = re.compile(r"^[A-Za-z]:$")

@dataclass
class Rules:
    check_dotdot: bool = True
    check_backslash: bool = True
    check_control: bool = True
    check_empty_segment: bool = True
    check_drive: bool = True
    check_lengths: bool = True
    sanitize_chars: bool = True
    sanitize_reserved: bool = True
    sanitize_trailing: bool = True
    sanitize_bidi: bool = True
    nfc: bool = True
    fold_collisions: bool = True
    reject_dup: bool = True
    reject_file_dir: bool = True


def validate_path(p, r=Rules()):
    """Return None for valid wire path, or one rejection reason."""
    if p == "": return "empty"
    if p.startswith("/"): return "absolute"
    if r.check_backslash and "\\" in p: return "backslash"
    if r.check_control and any(ord(c) < 0x20 or ord(c) == 0x7F for c in p): return "control"
    segs = p.split("/")
    for s in segs:
        if r.check_empty_segment and not s: return "empty_segment"
        if r.check_dotdot and s in (".", ".."): return "dot_segment"
    if r.check_drive and DRIVE.match(segs[0]): return "drive_letter"
    if r.check_lengths:
        if any(len(s.encode("utf-8")) > MAX_SEGMENT_BYTES for s in segs): return "segment_too_long"
        if len(p.encode("utf-8")) > MAX_PATH_BYTES: return "path_too_long"
    return None


def sanitize_segment(seg, profile, r=Rules()):
    if r.nfc: seg = unicodedata.normalize("NFC", seg)
    if profile != "windows": return seg
    out = []
    for c in seg:
        if r.sanitize_chars and (c in WIN_BAD or ord(c) < 0x20): out.append("_")
        elif r.sanitize_bidi and c in BIDI: out.append("_")
        else: out.append(c)
    s = "".join(out)
    if r.sanitize_trailing:
        s = s.rstrip(" .")
        if not s: s = "_"
    if r.sanitize_reserved:
        stem = s.split(".")[0].rstrip(" ").upper()
        if stem in RESERVED: s = "_" + s
    return s


def _key(s, profile, r):
    return s.casefold() if profile == "windows" and r.fold_collisions else s


def _numbered(name, n):
    i = name.rfind(".")
    if i <= 0: return f"{name} ({n})"
    return f"{name[:i]} ({n}){name[i:]}"


def plan_manifest(entries, profile, r=Rules()):
    """Plan all destinations before writing anything.

    Returns ('reject', reason) or ('ok', final relative paths in input order).
    Namespace reservation runs in *two passes*: first merge/register every
    explicit and implicit directory, then assign file names. A file conflicting
    with ANY directory path after local sanitization/case folding rejects the
    entire manifest; file-to-file collisions are numbered without clobbering
    directories, including directories appearing later in the manifest.
    """
    if profile not in ("windows", "posix"):
        raise ValueError("unknown destination profile")
    for t, p in entries:
        if t not in ("file", "dir"): return ("reject", "invalid_entry_type")
        why = validate_path(p, r)
        if why: return ("reject", why)
    if r.reject_dup:
        seen = set()
        for t, p in entries:
            if (t, p) in seen: return ("reject", "duplicate_path")
            seen.add((t, p))
    if r.reject_file_dir:
        files = {p for t, p in entries if t == "file"}
        dirs = {p for t, p in entries if t == "dir"}
        parents = {"/".join(p.split("/")[:i]) for _, p in entries for i in range(1, len(p.split("/")))}
        if files & (dirs | parents): return ("reject", "file_dir_conflict")

    # Directory merging uses canonical, sanitized parent names. Reserve all
    # directories (explicit and implicit) before assigning *any* file name.
    directory_names = {}                  # (canonical_parent, normalized_segment) -> displayed_segment
    reserved_directories = set()         # canonical path keys
    prepared = []                        # (kind, parent_display, basename_display, final_dir_display)
    for t, wire_path in entries:
        segments = [sanitize_segment(s, profile, r) for s in wire_path.split("/")]
        parent = ""
        for i, s in enumerate(segments):
            is_dir = i < len(segments) - 1 or t == "dir"
            if is_dir:
                k = (parent, _key(s, profile, r))
                display = directory_names.setdefault(k, s)
                path = f"{parent}/{display}" if parent else display
                reserved_directories.add(_key(path, profile, r))
                parent = path
            else:
                if r.reject_file_dir and _key(f"{parent}/{s}" if parent else s, profile, r) in reserved_directories:
                    return ("reject", "file_dir_conflict")
                prepared.append((t, parent, s, None))
        if t == "dir": prepared.append((t, None, None, parent))

    used_files = set()
    result = []
    for t, parent, basename, directory in prepared:
        if t == "dir":
            result.append(directory)
            continue
        base = f"{parent}/{basename}" if parent else basename
        if r.reject_file_dir and _key(base, profile, r) in reserved_directories:
            return ("reject", "file_dir_conflict")
        candidate, n = basename, 0
        while True:
            path = f"{parent}/{candidate}" if parent else candidate
            key = _key(path, profile, r)
            if key not in used_files and key not in reserved_directories: break
            n += 1
            candidate = _numbered(basename, n)
            if r.check_lengths and len(candidate.encode("utf-8")) > MAX_SEGMENT_BYTES:
                return ("reject", "segment_too_long")
        if r.check_lengths and len(path.encode("utf-8")) > MAX_PATH_BYTES:
            return ("reject", "path_too_long")
        used_files.add(key)
        result.append(path)

    # Validate every emitted path too: sanitization may expand names, and
    # numbered collision suffixes can exceed filesystem/wire length budgets.
    if r.check_lengths:
        for path in result:
            if any(len(s.encode("utf-8")) > MAX_SEGMENT_BYTES for s in path.split("/")):
                return ("reject", "segment_too_long")
            if len(path.encode("utf-8")) > MAX_PATH_BYTES:
                return ("reject", "path_too_long")
    return ("ok", result)


def plan_single(p, profile, r=Rules()):
    return plan_manifest([("file", p)], profile, r)
