export namespace desktopbackend {
	
	export class UpdateInfo {
	    available: boolean;
	    current: string;
	    latest: string;
	    url: string;
	    checkedAt?: string;
	    releasedAt?: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new UpdateInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.available = source["available"];
	        this.current = source["current"];
	        this.latest = source["latest"];
	        this.url = source["url"];
	        this.checkedAt = source["checkedAt"];
	        this.releasedAt = source["releasedAt"];
	        this.error = source["error"];
	    }
	}
	export class Job {
	    id: string;
	    kind: string;
	    target: string;
	    state: string;
	    message: string;
	    startedAt: string;
	    finishedAt?: string;
	
	    static createFrom(source: any = {}) {
	        return new Job(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.kind = source["kind"];
	        this.target = source["target"];
	        this.state = source["state"];
	        this.message = source["message"];
	        this.startedAt = source["startedAt"];
	        this.finishedAt = source["finishedAt"];
	    }
	}
	export class Sandbox {
	    name: string;
	    status: string;
	    host: string;
	    hostname: string;
	    username: string;
	    privateKeyPath: string;
	    hasPrivateKey: boolean;
	    backend: string;
	    createdAt: string;
	    cpus: number;
	    memoryGB: number;
	    diskGB: number;
	    imported: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Sandbox(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.status = source["status"];
	        this.host = source["host"];
	        this.hostname = source["hostname"];
	        this.username = source["username"];
	        this.privateKeyPath = source["privateKeyPath"];
	        this.hasPrivateKey = source["hasPrivateKey"];
	        this.backend = source["backend"];
	        this.createdAt = source["createdAt"];
	        this.cpus = source["cpus"];
	        this.memoryGB = source["memoryGB"];
	        this.diskGB = source["diskGB"];
	        this.imported = source["imported"];
	    }
	}
	export class Requirement {
	    name: string;
	    status: string;
	    details: string;
	    installed: boolean;
	    description: string;
	
	    static createFrom(source: any = {}) {
	        return new Requirement(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.status = source["status"];
	        this.details = source["details"];
	        this.installed = source["installed"];
	        this.description = source["description"];
	    }
	}
	export class AppState {
	    appVersion: string;
	    platform: string;
	    requirements: Requirement[];
	    instances: Sandbox[];
	    jobs: Job[];
	    update?: UpdateInfo;
	
	    static createFrom(source: any = {}) {
	        return new AppState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.appVersion = source["appVersion"];
	        this.platform = source["platform"];
	        this.requirements = this.convertValues(source["requirements"], Requirement);
	        this.instances = this.convertValues(source["instances"], Sandbox);
	        this.jobs = this.convertValues(source["jobs"], Job);
	        this.update = this.convertValues(source["update"], UpdateInfo);
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
	export class CreateSandboxInput {
	    name: string;
	    cpus: number;
	    memoryGB: number;
	    diskGB: number;
	    selfSigned: boolean;
	
	    static createFrom(source: any = {}) {
	        return new CreateSandboxInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.cpus = source["cpus"];
	        this.memoryGB = source["memoryGB"];
	        this.diskGB = source["diskGB"];
	        this.selfSigned = source["selfSigned"];
	    }
	}
	export class DiffEntry {
	    kind: string;
	    group: string;
	    value: string;
	
	    static createFrom(source: any = {}) {
	        return new DiffEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.group = source["group"];
	        this.value = source["value"];
	    }
	}
	
	
	export class SSHKeys {
	    privateKey: string;
	    publicKey: string;
	
	    static createFrom(source: any = {}) {
	        return new SSHKeys(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.privateKey = source["privateKey"];
	        this.publicKey = source["publicKey"];
	    }
	}
	
	export class ServerDiff {
	    checkpoint: string;
	    capturedAt: string;
	    fetchedAt: string;
	    total: number;
	    changes: DiffEntry[];
	
	    static createFrom(source: any = {}) {
	        return new ServerDiff(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.checkpoint = source["checkpoint"];
	        this.capturedAt = source["capturedAt"];
	        this.fetchedAt = source["fetchedAt"];
	        this.total = source["total"];
	        this.changes = this.convertValues(source["changes"], DiffEntry);
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
	export class ServerLogEntry {
	    id: string;
	    category: string;
	    timestamp?: string;
	    level: string;
	    source: string;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new ServerLogEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.category = source["category"];
	        this.timestamp = source["timestamp"];
	        this.level = source["level"];
	        this.source = source["source"];
	        this.message = source["message"];
	    }
	}
	export class ServerLogs {
	    fetchedAt: string;
	    entries: ServerLogEntry[];
	
	    static createFrom(source: any = {}) {
	        return new ServerLogs(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fetchedAt = source["fetchedAt"];
	        this.entries = this.convertValues(source["entries"], ServerLogEntry);
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
	export class SnapshotEntry {
	    name: string;
	    label: string;
	    comment: string;
	    parent: string;
	    checkpoint: boolean;
	    latest: boolean;
	    current: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SnapshotEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.label = source["label"];
	        this.comment = source["comment"];
	        this.parent = source["parent"];
	        this.checkpoint = source["checkpoint"];
	        this.latest = source["latest"];
	        this.current = source["current"];
	    }
	}
	export class SnapshotList {
	    entries: SnapshotEntry[];
	    hasBaseline: boolean;
	    baselineLabel?: string;
	    baselineAt?: string;
	
	    static createFrom(source: any = {}) {
	        return new SnapshotList(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.entries = this.convertValues(source["entries"], SnapshotEntry);
	        this.hasBaseline = source["hasBaseline"];
	        this.baselineLabel = source["baselineLabel"];
	        this.baselineAt = source["baselineAt"];
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
	
	export class UpdateSandboxInput {
	    name: string;
	    cpus: number;
	    memoryGB: number;
	    diskGB: number;
	
	    static createFrom(source: any = {}) {
	        return new UpdateSandboxInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.cpus = source["cpus"];
	        this.memoryGB = source["memoryGB"];
	        this.diskGB = source["diskGB"];
	    }
	}

}

