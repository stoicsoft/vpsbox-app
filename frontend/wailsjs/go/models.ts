export namespace desktopbackend {
	
	export class UpdateDownload {
	    state: string;
	    version?: string;
	    percent: number;
	    downloaded: number;
	    total: number;
	    error?: string;
	    jobId?: string;
	
	    static createFrom(source: any = {}) {
	        return new UpdateDownload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.state = source["state"];
	        this.version = source["version"];
	        this.percent = source["percent"];
	        this.downloaded = source["downloaded"];
	        this.total = source["total"];
	        this.error = source["error"];
	        this.jobId = source["jobId"];
	    }
	}
	export class UpdateInfo {
	    available: boolean;
	    current: string;
	    latest: string;
	    url: string;
	    checkedAt?: string;
	    releasedAt?: string;
	    error?: string;
	    assetName?: string;
	    assetURL?: string;
	    assetSize?: number;
	    canInstall: boolean;
	    blocker?: string;
	
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
	        this.assetName = source["assetName"];
	        this.assetURL = source["assetURL"];
	        this.assetSize = source["assetSize"];
	        this.canInstall = source["canInstall"];
	        this.blocker = source["blocker"];
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
	    log?: string[];
	
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
	        this.log = source["log"];
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
	    updateDownload: UpdateDownload;
	
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
	        this.updateDownload = this.convertValues(source["updateDownload"], UpdateDownload);
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
	export class Bucket {
	    name: string;
	    createdAt: string;
	
	    static createFrom(source: any = {}) {
	        return new Bucket(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.createdAt = source["createdAt"];
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
	export class DeployTemplate {
	    id: string;
	    name: string;
	    summary: string;
	    category: string;
	    kind: string;
	    icon: string;
	    port: number;
	    minMemoryMB: number;
	    note?: string;
	
	    static createFrom(source: any = {}) {
	        return new DeployTemplate(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.summary = source["summary"];
	        this.category = source["category"];
	        this.kind = source["kind"];
	        this.icon = source["icon"];
	        this.port = source["port"];
	        this.minMemoryMB = source["minMemoryMB"];
	        this.note = source["note"];
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
	export class TransferCompose {
	    name: string;
	    running: boolean;
	
	    static createFrom(source: any = {}) {
	        return new TransferCompose(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.running = source["running"];
	    }
	}
	export class TransferVolume {
	    name: string;
	    sizeMB: number;
	
	    static createFrom(source: any = {}) {
	        return new TransferVolume(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.sizeMB = source["sizeMB"];
	    }
	}
	export class ImportPreview {
	    server: string;
	    os: string;
	    sandboxName: string;
	    cpus: number;
	    memoryGB: number;
	    diskGB: number;
	    packages: number;
	    estimatedMB: number;
	    volumes: TransferVolume[];
	    compose: TransferCompose[];
	
	    static createFrom(source: any = {}) {
	        return new ImportPreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.server = source["server"];
	        this.os = source["os"];
	        this.sandboxName = source["sandboxName"];
	        this.cpus = source["cpus"];
	        this.memoryGB = source["memoryGB"];
	        this.diskGB = source["diskGB"];
	        this.packages = source["packages"];
	        this.estimatedMB = source["estimatedMB"];
	        this.volumes = this.convertValues(source["volumes"], TransferVolume);
	        this.compose = this.convertValues(source["compose"], TransferCompose);
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
	export class ImportVPSInput {
	    user: string;
	    host: string;
	    port: number;
	    keyPath: string;
	    name: string;
	
	    static createFrom(source: any = {}) {
	        return new ImportVPSInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.user = source["user"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.keyPath = source["keyPath"];
	        this.name = source["name"];
	    }
	}
	
	export class LiveApp {
	    templateId?: string;
	    name: string;
	    icon?: string;
	    port: number;
	    url: string;
	    running: boolean;
	    container?: string;
	
	    static createFrom(source: any = {}) {
	        return new LiveApp(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.templateId = source["templateId"];
	        this.name = source["name"];
	        this.icon = source["icon"];
	        this.port = source["port"];
	        this.url = source["url"];
	        this.running = source["running"];
	        this.container = source["container"];
	    }
	}
	export class ObjectStore {
	    running: boolean;
	    installed: boolean;
	    endpoint: string;
	    hostEndpoint: string;
	    region: string;
	    accessKey: string;
	    secretKey: string;
	    buckets: Bucket[];
	
	    static createFrom(source: any = {}) {
	        return new ObjectStore(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.running = source["running"];
	        this.installed = source["installed"];
	        this.endpoint = source["endpoint"];
	        this.hostEndpoint = source["hostEndpoint"];
	        this.region = source["region"];
	        this.accessKey = source["accessKey"];
	        this.secretKey = source["secretKey"];
	        this.buckets = this.convertValues(source["buckets"], Bucket);
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
	export class PushInput {
	    name: string;
	    user: string;
	    host: string;
	    port: number;
	    keyPath: string;
	
	    static createFrom(source: any = {}) {
	        return new PushInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.user = source["user"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.keyPath = source["keyPath"];
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
	
	export class TransferPath {
	    path: string;
	    sizeMB: number;
	
	    static createFrom(source: any = {}) {
	        return new TransferPath(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.sizeMB = source["sizeMB"];
	    }
	}
	export class TransferPlan {
	    source: string;
	    dest: string;
	    packages: string[];
	    services: string[];
	    users: string[];
	    paths: TransferPath[];
	    volumes: TransferVolume[];
	    compose: TransferCompose[];
	    notes: string[];
	    estimatedMB: number;
	
	    static createFrom(source: any = {}) {
	        return new TransferPlan(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source = source["source"];
	        this.dest = source["dest"];
	        this.packages = source["packages"];
	        this.services = source["services"];
	        this.users = source["users"];
	        this.paths = this.convertValues(source["paths"], TransferPath);
	        this.volumes = this.convertValues(source["volumes"], TransferVolume);
	        this.compose = this.convertValues(source["compose"], TransferCompose);
	        this.notes = source["notes"];
	        this.estimatedMB = source["estimatedMB"];
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
	export class WorkspaceMember {
	    instance: string;
	    privateIp: string;
	    hostname: string;
	    status: string;
	
	    static createFrom(source: any = {}) {
	        return new WorkspaceMember(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.instance = source["instance"];
	        this.privateIp = source["privateIp"];
	        this.hostname = source["hostname"];
	        this.status = source["status"];
	    }
	}
	export class Workspace {
	    name: string;
	    cidr: string;
	    gateway: string;
	    members: WorkspaceMember[];
	
	    static createFrom(source: any = {}) {
	        return new Workspace(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.cidr = source["cidr"];
	        this.gateway = source["gateway"];
	        this.members = this.convertValues(source["members"], WorkspaceMember);
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

