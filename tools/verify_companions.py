#!/usr/bin/env python3
"""Verify that the LANVault companion files agree with each other.

Usage:  python3 tools/verify_companions.py [--prd PRD.md] [--self-test]

Checks (each prints PASS or FAIL):
  constants   protocol-constants.json is internally consistent
  protocol    PROTOCOL.md constants, frame types and error codes equal the JSON
  vectors     manifest hash, frames, bitmap, identity and pairing URI are recomputed from first principles
  tokens      every contrast pair in design-tokens.json meets its minimum ratio
  settings    settings-schema.json defaults are valid and reference real constants
  paths       the hand-authored security corpus matches the reference oracle
  prd         (optional) tokens, settings and constants tables in an exported PRD Markdown file match the JSON
Self-test: builds deliberately broken variants and requires the checks to FAIL on each one.
Exit code 0 only if every check passes.
"""
import argparse, base64, copy, hashlib, json, os, re, ssl, struct, sys, unicodedata
from dataclasses import fields
ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
sys.path.insert(0, os.path.join(ROOT, "tools", "ref"))
import path_rules as pr
import manifest_parts as mp

def load(*p): return json.load(open(os.path.join(ROOT, *p), encoding="utf-8"))
def text(*p): return open(os.path.join(ROOT, *p), encoding="utf-8").read()

# ---------------- checks (each returns a list of problems) ----------------
def check_constants(C):
    p=[]; c={k:v["value"] for k,v in C["constants"].items()}
    if c["MAX_DATA_FRAME"] != c["CHUNK_SIZE_MAX"] + c["DATA_FRAME_OVERHEAD"]: p.append("MAX_DATA_FRAME != CHUNK_SIZE_MAX + DATA_FRAME_OVERHEAD")
    if not c["CHUNK_SIZE_MIN"] <= c["CHUNK_SIZE_DEFAULT"] <= c["CHUNK_SIZE_MAX"]: p.append("CHUNK_SIZE_DEFAULT outside min..max")
    if c["DATA_STREAMS_DEFAULT"] > c["DATA_STREAMS_MAX"]: p.append("DATA_STREAMS_DEFAULT > DATA_STREAMS_MAX")
    if c["DEAD_PEER_TIMEOUT_MS"] <= c["PING_INTERVAL_MS"]: p.append("DEAD_PEER_TIMEOUT_MS must exceed PING_INTERVAL_MS")
    if c["DEFAULT_PORT"] == c["BEACON_PORT"]: p.append("ports collide")
    for name,vals in (("frame_types",C["frame_types"]),("error_codes",C["error_codes"])):
        if len(set(vals.values())) != len(vals): p.append(f"duplicate value in {name}")
    return p

def check_protocol(C, md):
    p=[]
    sec1 = md[md.index("## 1. Constants"):md.index("## 2. Identity")]
    got = {m.group(1): m.group(2).strip() for m in re.finditer(r"^\|\s*`([A-Z0-9_]+)`\s*\|\s*([^|]+?)\s*\|", sec1, re.M)}
    for k,v in C["constants"].items():
        if k not in got: p.append(f"PROTOCOL.md section 1 lacks {k}"); continue
        if str(v["value"]) != got[k].replace(",",""): p.append(f"{k}: JSON {v['value']} vs PROTOCOL.md {got[k]}")
    for k in got:
        if k not in C["constants"]: p.append(f"PROTOCOL.md has {k} which is not in the JSON")
    sec5 = md[md.index("### 5.3 Frame types"):md.index("## 6. Session lifecycle")]
    frames = {m.group(2): int(m.group(1),16) for m in re.finditer(r"^\|\s*`0x([0-9A-Fa-f]{2})`\s*\|\s*([A-Z_]+)\s*\|", sec5, re.M)}
    if frames != C["frame_types"]: p.append("frame type table differs: " + str(set(frames.items()) ^ set(C["frame_types"].items())))
    sec12 = md[md.index("## 12. Error codes"):md.index("## 13. Versioning")]
    errs = {m.group(2): int(m.group(1)) for m in re.finditer(r"^\|\s*(\d+)\s*\|\s*`([A-Z_]+)`\s*\|", sec12, re.M)}
    if errs != C["error_codes"]: p.append("error code table differs: " + str(set(errs.items()) ^ set(C["error_codes"].items())))
    hello = re.search(r'### 7\.1 HELLO / HELLO_OK[\s\S]*?"version":\s*\{"major":\s*(\d+),\s*"minor":\s*(\d+)\}', md)
    if not hello: p.append("HELLO JSON version example missing")
    elif (int(hello.group(1)),int(hello.group(2))) != (C["constants"]["PROTOCOL_MAJOR"]["value"],C["constants"]["PROTOCOL_MINOR"]["value"]):
        p.append("HELLO example version disagrees with protocol-constants.json")
    return p

def frame(t, payload=b""): return struct.pack(">I", 1+len(payload)) + bytes([t]) + payload

def check_vectors(C, V):
    p=[]; ft=C["frame_types"]
    canon="".join(f"{e['type']}\t{e['path']}\t{e['size']}\t{e['mtime_ms']}\n" for e in V["manifest"]["entries"]).encode()
    if canon.hex()!=V["manifest"]["canonical_utf8_hex"]: p.append("manifest canonical bytes differ")
    if hashlib.sha256(canon).hexdigest()!=V["manifest"]["manifest_hash"]: p.append("manifest_hash differs")
    for f in V["frames"]["frames"]:
        t=f["type"]
        if t=="PING": exp=frame(ft["PING"])
        elif t=="DATA":
            ch=bytes.fromhex(f["chunk_hex"])
            if hashlib.sha256(ch).hexdigest()!=f["chunk_sha256"]: p.append("DATA chunk hash differs")
            exp=frame(ft["DATA"], struct.pack(">II",f["file_index"],f["chunk_index"])+ch+bytes.fromhex(f["chunk_sha256"]))
        elif t in ("CHUNK_ACK","CHUNK_NEED"): exp=frame(ft[t], b"".join(struct.pack(">II",a,b) for a,b in f["pairs"]))
        else: p.append(f"unknown frame type {t}"); continue
        if exp.hex()!=f["frame_hex"]: p.append(f"frame {f['name']}: expected {exp.hex()} got {f['frame_hex']}")
        if int.from_bytes(exp[:4],"big")!=len(exp)-4: p.append(f"frame {f['name']}: length field wrong")
    b=V["bitmap"]; bm=bytearray((b["chunk_count"]+7)//8)
    for i in b["completed"]: bm[i//8]|=1<<(i%8)
    if bm.hex()!=b["bitmap_hex"] or base64.b64encode(bytes(bm)).decode()!=b["bitmap_base64"]: p.append("bitmap encoding differs")
    def bitmap_ok(cc, b64):
        try: raw=base64.b64decode(b64, validate=True)
        except Exception: return False
        if len(raw)!=(cc+7)//8: return False
        return not any((raw[i//8]>>(i%8))&1 for i in range(cc, len(raw)*8))
    if not bitmap_ok(b["chunk_count"], b["bitmap_base64"]): p.append("valid bitmap rejected by sanity check")
    for bad in b["invalid_examples"]:
        if bitmap_ok(bad["chunk_count"], bad["bitmap_base64"]): p.append(f"invalid bitmap accepted: {bad['note']}")
    idv=V["identity"]; der=ssl.PEM_cert_to_DER_cert(idv["certificate_pem"])
    fp=hashlib.sha256(der).hexdigest()
    if fp!=idv["fingerprint"] or fp[:32]!=idv["device_id"] or idv["short_fingerprint_grouped"]!=" ".join(fp[i:i+4] for i in (0,4)): p.append("identity vector differs")
    u=V["pairing"]["qr_uri_example"]; q=dict(x.split("=",1) for x in u.split("?",1)[1].split("&"))
    e=V["pairing"]["expected"]
    if not (q["fp"]==e["fp"]==idv["fingerprint"] and q["ip"].split(",")==e["ip"] and int(q["port"])==e["port"] and len(base64.urlsafe_b64decode(q["tok"]+"=="))==e["tok_bytes"]==C["constants"]["PAIR_TOKEN_BYTES"]["value"]): p.append("pairing URI vector differs")
    return p

def lum(h):
    h=h.lstrip("#"); r,g,b=[int(h[i:i+2],16)/255 for i in (0,2,4)]
    f=lambda c: c/12.92 if c<=0.03928 else ((c+0.055)/1.055)**2.4
    return 0.2126*f(r)+0.7152*f(g)+0.0722*f(b)
def ratio(a,b):
    x,y=sorted((lum(a),lum(b)),reverse=True); return (x+0.05)/(y+0.05)

def check_tokens(T):
    p=[]
    for theme in ("dark","light"):
        col=T["color"][theme]
        for pair in T["contrast_pairs"]:
            r=ratio(col[pair["fg"]], col[pair["bg"]])
            if r<pair["min"]: p.append(f"{theme}: {pair['fg']} on {pair['bg']} = {r:.2f} < {pair['min']}")
    if T["color"]["dark"].keys()!=T["color"]["light"].keys(): p.append("dark and light token names differ")
    return p

def check_settings(S, C):
    p=[]; keys=set(); c=C["constants"]
    for s in S["settings"]:
        if s["key"] in keys: p.append(f"duplicate key {s['key']}")
        keys.add(s["key"])
        if s["priority"] not in ("P0","P1","P2"): p.append(f"{s['key']}: bad priority")
        if s["platform"] not in ("both","windows","android"): p.append(f"{s['key']}: bad platform")
        d,o=s["default"],s["options"]
        if s["type"] in ("enum","bool") and isinstance(o,list) and not (isinstance(d,str) and d.startswith("@")) and d not in o: p.append(f"{s['key']}: default {d!r} not in options")
        for ref in [d]+([o.get("max")] if isinstance(o,dict) else []):
            if isinstance(ref,str) and ref.isupper() and "_" in ref and ref not in c: p.append(f"{s['key']}: unknown constant {ref}")
    return p

def check_paths(P, rules=pr.Rules()):
    p=[]
    for c in P["paths"]:
        why=pr.validate_path(c["path"], rules)
        if c["validate"]=="reject":
            if why!=c["reason"]: p.append(f"{c['id']}: expected reject({c['reason']}) got {why}")
        else:
            if why: p.append(f"{c['id']}: unexpectedly rejected ({why})"); continue
            for prof in ("windows","posix"):
                res=pr.plan_single(c["path"],prof,rules)
                if res!=("ok",[c[prof]]): p.append(f"{c['id']} [{prof}]: expected {c[prof]!r} got {res}")
    for m in P["manifests"]:
        ent=[(e["type"],e["path"]) for e in m["entries"]]
        for prof in ("windows","posix"):
            res=pr.plan_manifest(ent,prof,rules)
            if "expected" in m:
                want=m["expected"][prof]
                exp=("reject",want["reason"]) if want["result"]=="reject" else ("ok",want["paths"])
            else:
                exp=("reject",m["reason"]) if m["result"]=="reject" else ("ok",m[prof])
            if res!=exp: p.append(f"{m['id']} [{prof}]: expected {exp} got {res}")
            if res[0]=="ok":
                # Invariants independently validate the oracle's final namespace.
                kinds=[t for t,_ in ent]
                keys=[(v.casefold() if prof=="windows" and rules.fold_collisions else v) for v in res[1]]
                filepaths=[k for k,t in zip(keys,kinds) if t=="file"]
                dirpaths={k for k,t in zip(keys,kinds) if t=="dir"}
                implicit={"/".join(k.split("/")[:i]) for k in keys for i in range(1,len(k.split("/")))}
                if len(filepaths)!=len(set(filepaths)): p.append(f"{m['id']} [{prof}]: file output names overlap")
                if set(filepaths) & (dirpaths | implicit): p.append(f"{m['id']} [{prof}]: file aliases directory (explicit/implicit)")
    return p

def check_manifest_batches(C):
    """Exercise both independent MANIFEST_PART limits using UTF-8 encoded sizes."""
    p=[]
    c={k:v["value"] for k,v in C["constants"].items()}
    max_entries=c["MANIFEST_PART_MAX_ENTRIES"]; max_frame=c["MAX_CONTROL_FRAME"]
    def make_entry(i, name):
        return {"index":i,"type":"file","path":name,"size":1,"mtime_ms":1730000000000}
    samples=[make_entry(i,"/".join(["漢"*60]*6)+f"/{i}.txt") for i in range(max_entries+10)]
    # Simulates legally long path segments and forces a byte cap before count cap.
    # 60 Han characters = 180 bytes/segment; whole path < 4096 bytes.
    try: parts=mp.batch_manifest(samples,max_entries,max_frame)
    except ValueError as e: return [f"unable to batch valid manifest entries: {e}"]
    if [e for batch in parts for e in batch] != samples: p.append("manifest parts reorder/lose entries")
    if len(parts)<2: p.append("oversized manifest not divided into multiple parts")
    if not any(len(batch) < max_entries for batch in parts[:-1]): p.append("missing byte-based split before 1000-entry count")
    for i,batch in enumerate(parts):
        if len(batch)>max_entries: p.append(f"manifest part {i} exceeds entry-count cap")
        if mp.encoded_frame_size(batch)>max_frame: p.append(f"manifest part {i} exceeds control-byte cap")
    # One entry may legitimately exceed a *test* transport cap; fail safely.
    if mp.encoded_frame_size([samples[0]]) < max_frame:
        try: mp.batch_manifest([samples[0]],max_entries,mp.encoded_frame_size([samples[0]])-1)
        except ValueError: pass
        else: p.append("oversized individual entry not rejected")
    if mp.batch_manifest([],max_entries,max_frame) != []: p.append("empty manifest produced nonempty part")
    return p

def check_prd(md, T, S, C):
    p=[]
    # Accept Pandoc's escaped # character in Markdown tables. The old regex
    # mistakenly interpreted every escaped #HEX as a missing color token.
    table_rows=[]
    for ln in md.splitlines():
        if not ln.strip().startswith("|") or not ln.strip().endswith("|"): continue
        cells=[x.strip().replace(r'\#','#') for x in ln.strip()[1:-1].split('|')]
        if not all(re.fullmatch(r'[-: ]+',v) for v in cells): table_rows.append(cells)
    colors={r[0]:(r[1],r[2]) for r in table_rows if len(r)>=3 and r[1].startswith('#') and r[2].startswith('#')}
    for name, (d,l) in {k:(T["color"]["dark"][k],T["color"]["light"][k]) for k in T["color"]["dark"]}.items():
        if name not in colors: p.append(f"PRD lacks token row {name}")
        elif tuple(x.upper() for x in colors[name])!=(d.upper(),l.upper()): p.append(f"PRD token {name} differs")
    rows=[r for r in table_rows if len(r)==6]
    byname={r[1]:r for r in rows}
    for s in S["settings"]:
        r=byname.get(s["label"])
        if not r: p.append(f"PRD settings table lacks {s['label']}"); continue
        if r[5]!=s["priority"]: p.append(f"PRD priority for {s['label']}: {r[5]} vs {s['priority']}")
        if r[4].lower()!=s["platform"]: p.append(f"PRD platform for {s['label']}: {r[4]} vs {s['platform']}")
    vers=f'wire protocol v{C["constants"]["PROTOCOL_MAJOR"]["value"]}.{C["constants"]["PROTOCOL_MINOR"]["value"]}'
    head=md[:1000].lower()
    if vers not in head: p.append(f"PRD header must name {vers}")
    return p

# ---------------- driver ----------------
def run_all(C,T,S,V,P,md,prd=None):
    res={"constants":check_constants(C),"protocol":check_protocol(C,md),"vectors":check_vectors(C,V),
         "tokens":check_tokens(T),"settings":check_settings(S,C),"paths":check_paths(P),"manifest_frames":check_manifest_batches(C)}
    if prd: res["prd"]=check_prd(prd,T,S,C)
    return res

def self_test(C,T,S,V,P,md):
    """Every mutation must be caught; a check that cannot fail proves nothing."""
    ok=True
    def expect_fail(label, problems):
        nonlocal ok
        status="caught" if problems else "NOT CAUGHT"
        if not problems: ok=False
        print(f"  mutant {label:<46} {status}")
    m=copy.deepcopy(C); m["constants"]["CHUNK_SIZE_MAX"]["value"]+=1; expect_fail("constants: CHUNK_SIZE_MAX changed", check_constants(m))
    m=copy.deepcopy(C); m["constants"]["DEFAULT_PORT"]["value"]=1; expect_fail("protocol: DEFAULT_PORT differs from PROTOCOL.md", check_protocol(m,md))
    m=copy.deepcopy(C); m["frame_types"]["PING"]=124; expect_fail("protocol: PING type differs", check_protocol(m,md))
    m=copy.deepcopy(C); m["error_codes"]["BUSY"]=99; expect_fail("protocol: BUSY code differs", check_protocol(m,md))
    m=copy.deepcopy(V); m["manifest"]["manifest_hash"]="0"*64; expect_fail("vectors: manifest hash corrupted", check_vectors(C,m))
    m=copy.deepcopy(V); m["frames"]["frames"][1]["frame_hex"]=m["frames"]["frames"][1]["frame_hex"][:-2]+"00"; expect_fail("vectors: DATA frame byte flipped", check_vectors(C,m))
    m=copy.deepcopy(V); m["bitmap"]["bitmap_base64"]="BwA="; expect_fail("vectors: bitmap changed", check_vectors(C,m))
    m=copy.deepcopy(C); m["constants"]["PROTOCOL_MINOR"]["value"]+=1; expect_fail("protocol: HELLO minor version disagrees", check_protocol(m,md))
    m=copy.deepcopy(T); m["color"]["light"]["text.secondary"]="#AAAAAA"; expect_fail("tokens: low-contrast secondary text", check_tokens(m))
    m=copy.deepcopy(S); m["settings"][1]["default"]="sepia"; expect_fail("settings: default not in options", check_settings(m,C))
    m=copy.deepcopy(C); m["constants"]["MAX_CONTROL_FRAME"]["value"]=100; expect_fail("manifest: cap too small to send an entry", check_manifest_batches(m))
    for flag in [f.name for f in fields(pr.Rules)]:
        r=pr.Rules(**{flag: False}); expect_fail(f"paths: rule '{flag}' disabled", check_paths(P,r))
    return ok

def main():
    ap=argparse.ArgumentParser(); ap.add_argument("--prd"); ap.add_argument("--self-test",action="store_true"); a=ap.parse_args()
    C=load("docs","protocol-constants.json"); T=load("docs","design-tokens.json"); S=load("docs","settings-schema.json")
    V={"manifest":load("testvectors","manifest_hash.json"),"frames":load("testvectors","frames.json"),"bitmap":load("testvectors","bitmap.json"),
       "identity":load("testvectors","identity.json"),"pairing":load("testvectors","pairing_uri.json")}
    P=load("testvectors","security","path-corpus.json"); md=text("docs","PROTOCOL.md")
    prd=open(a.prd,encoding="utf-8").read() if a.prd else None
    res=run_all(C,T,S,V,P,md,prd); bad=0
    for k,v in res.items():
        print(f"{'PASS' if not v else 'FAIL'}  {k}")
        for line in v: print("      -",line)
        bad+=bool(v)
    n=len(P["paths"])+len(P["manifests"]); print(f"      ({n} path and manifest cases, {len(C['constants'])} constants, {len(S['settings'])} settings)")
    if a.self_test:
        print("Self-test (each mutant must be caught):")
        if not self_test(C,T,S,V,P,md): bad+=1; print("FAIL  self-test")
        else: print("PASS  self-test")
    sys.exit(1 if bad else 0)

if __name__=="__main__": main()
