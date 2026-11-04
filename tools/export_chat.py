#!/usr/bin/env python3
import json
import os
import re

LOG_FILE = r"C:\Users\arpan\.gemini\antigravity-cli\brain\01fa63ff-3b8a-4ec1-bfd5-ea0c11baeb3b\.system_generated\logs\transcript_full.jsonl"
OUT_MD = r"D:\Downloads\LANVault_Full_Chat_Session.md"

def export_chat():
    if not os.path.exists(LOG_FILE):
        print(f"Transcript not found at {LOG_FILE}")
        return

    with open(LOG_FILE, "r", encoding="utf-8") as f:
        lines = f.readlines()

    entries = []
    for line in lines:
        line = line.strip()
        if not line:
            continue
        try:
            data = json.loads(line)
            source = data.get("source")
            msg_type = data.get("type")
            content = data.get("content")
            created_at = data.get("created_at", "")

            # Filter user inputs and assistant responses
            if msg_type == "USER_INPUT" and source == "USER_EXPLICIT" and content:
                # Clean prompt tags if wrapped in <USER_REQUEST>
                cleaned = re.sub(r"^<USER_REQUEST>\s*", "", content.strip())
                cleaned = re.sub(r"\s*</USER_REQUEST>$", "", cleaned.strip())
                entries.append({
                    "role": "User",
                    "time": created_at,
                    "content": cleaned
                })
            elif msg_type == "PLANNER_RESPONSE" and content:
                entries.append({
                    "role": "Antigravity Assistant",
                    "time": created_at,
                    "content": content.strip()
                })
        except Exception:
            continue

    with open(OUT_MD, "w", encoding="utf-8") as out:
        out.write("# LANVault — Full Chat Session Export\n\n")
        out.write(f"**Session ID:** `01fa63ff-3b8a-4ec1-bfd5-ea0c11baeb3b`  \n")
        out.write(f"**Total Messages:** {len(entries)}  \n\n---\n\n")

        for idx, entry in enumerate(entries, 1):
            role_header = f"### #{idx} {entry['role']}"
            if entry.get("time"):
                role_header += f" *({entry['time']})*"
            out.write(f"{role_header}\n\n")
            out.write(f"{entry['content']}\n\n---\n\n")

    print(f"Exported {len(entries)} messages to: {OUT_MD}")

if __name__ == "__main__":
    export_chat()
