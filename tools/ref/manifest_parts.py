"""Specification reference for manifest batching, not a production wire codec."""
import json

def encoded_frame_size(entries):
    """Encoded control-frame length: 1 type byte plus compact UTF-8 JSON."""
    return 1 + len(json.dumps({'entries': entries}, ensure_ascii=False, separators=(',', ':')).encode('utf-8'))

def batch_manifest(entries, max_entries, max_frame_bytes):
    """Return manifest parts that fit BOTH frame length and entry-count caps."""
    if max_entries <= 0 or max_frame_bytes <= encoded_frame_size([]):
        raise ValueError('invalid frame or entry limits')
    parts, current = [], []
    for entry in entries:
        if encoded_frame_size([entry]) > max_frame_bytes:
            raise ValueError('single manifest entry exceeds control frame')
        if current and (len(current) >= max_entries or encoded_frame_size(current+[entry]) > max_frame_bytes):
            parts.append(current)
            current = []
        current.append(entry)
    if current:
        parts.append(current)
    return parts
