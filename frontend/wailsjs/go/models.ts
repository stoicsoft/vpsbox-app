export namespace desktopbackend {
	
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

