export namespace diag {
	
	export class DiagnosticResult {
	    name: string;
	    passed: boolean;
	    code?: string;
	    message: string;
	    detail?: string;
	
	    static createFrom(source: any = {}) {
	        return new DiagnosticResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.passed = source["passed"];
	        this.code = source["code"];
	        this.message = source["message"];
	        this.detail = source["detail"];
	    }
	}

}

export namespace discovery {
	
	export class DiscoveredPeer {
	    device_id: string;
	    name: string;
	    platform: string;
	    ips: string[];
	    port: number;
	    // Go type: time
	    last_seen: any;
	    source: string;
	
	    static createFrom(source: any = {}) {
	        return new DiscoveredPeer(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.device_id = source["device_id"];
	        this.name = source["name"];
	        this.platform = source["platform"];
	        this.ips = source["ips"];
	        this.port = source["port"];
	        this.last_seen = this.convertValues(source["last_seen"], null);
	        this.source = source["source"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace main {
	
	export class AppState {
	    device_id: string;
	    fingerprint: string;
	    short_fingerprint: string;
	    device_name: string;
	    local_ips: string[];
	    port: number;
	    save_dir: string;
	    visibility: string;
	
	    static createFrom(source: any = {}) {
	        return new AppState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.device_id = source["device_id"];
	        this.fingerprint = source["fingerprint"];
	        this.short_fingerprint = source["short_fingerprint"];
	        this.device_name = source["device_name"];
	        this.local_ips = source["local_ips"];
	        this.port = source["port"];
	        this.save_dir = source["save_dir"];
	        this.visibility = source["visibility"];
	    }
	}
	export class ScannedSelection {
	    items: transfer.ScannedItem[];
	    total_files: number;
	    total_bytes: number;
	    manifest_hash: string;
	
	    static createFrom(source: any = {}) {
	        return new ScannedSelection(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.items = this.convertValues(source["items"], transfer.ScannedItem);
	        this.total_files = source["total_files"];
	        this.total_bytes = source["total_bytes"];
	        this.manifest_hash = source["manifest_hash"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace protocol {
	
	export class ManifestEntry {
	    index: number;
	    type: string;
	    path: string;
	    size: number;
	    mtime_ms: number;
	
	    static createFrom(source: any = {}) {
	        return new ManifestEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.index = source["index"];
	        this.type = source["type"];
	        this.path = source["path"];
	        this.size = source["size"];
	        this.mtime_ms = source["mtime_ms"];
	    }
	}

}

export namespace store {
	
	export class TransferRecord {
	    transfer_id: string;
	    direction: string;
	    peer_fingerprint: string;
	    peer_name: string;
	    peer_device_id: string;
	    state: string;
	    chunk_size: number;
	    total_files: number;
	    total_bytes: number;
	    completed_files: number;
	    completed_bytes: number;
	    manifest_hash: string;
	    created_at: number;
	    updated_at: number;
	    error_message?: string;
	
	    static createFrom(source: any = {}) {
	        return new TransferRecord(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.transfer_id = source["transfer_id"];
	        this.direction = source["direction"];
	        this.peer_fingerprint = source["peer_fingerprint"];
	        this.peer_name = source["peer_name"];
	        this.peer_device_id = source["peer_device_id"];
	        this.state = source["state"];
	        this.chunk_size = source["chunk_size"];
	        this.total_files = source["total_files"];
	        this.total_bytes = source["total_bytes"];
	        this.completed_files = source["completed_files"];
	        this.completed_bytes = source["completed_bytes"];
	        this.manifest_hash = source["manifest_hash"];
	        this.created_at = source["created_at"];
	        this.updated_at = source["updated_at"];
	        this.error_message = source["error_message"];
	    }
	}
	export class TrustedDevice {
	    fingerprint: string;
	    device_id: string;
	    name: string;
	    platform: string;
	    paired_at: number;
	    auto_accept: boolean;
	    last_seen: number;
	
	    static createFrom(source: any = {}) {
	        return new TrustedDevice(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fingerprint = source["fingerprint"];
	        this.device_id = source["device_id"];
	        this.name = source["name"];
	        this.platform = source["platform"];
	        this.paired_at = source["paired_at"];
	        this.auto_accept = source["auto_accept"];
	        this.last_seen = source["last_seen"];
	    }
	}

}

export namespace transfer {
	
	export class ScannedItem {
	    SourceDiskPath: string;
	    Entry: protocol.ManifestEntry;
	
	    static createFrom(source: any = {}) {
	        return new ScannedItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.SourceDiskPath = source["SourceDiskPath"];
	        this.Entry = this.convertValues(source["Entry"], protocol.ManifestEntry);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

