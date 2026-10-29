export namespace app {
	
	export class AuditQueryDTO {
	    tool: string;
	    project: string;
	    risk: string;
	    search: string;
	    ok?: boolean;
	    sinceMs?: number;
	    untilMs?: number;
	    limit?: number;
	    offset?: number;
	
	    static createFrom(source: any = {}) {
	        return new AuditQueryDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tool = source["tool"];
	        this.project = source["project"];
	        this.risk = source["risk"];
	        this.search = source["search"];
	        this.ok = source["ok"];
	        this.sinceMs = source["sinceMs"];
	        this.untilMs = source["untilMs"];
	        this.limit = source["limit"];
	        this.offset = source["offset"];
	    }
	}
	export class CollectionDefaults {
	    headers: collection.KV[];
	    auth?: collection.Auth;
	
	    static createFrom(source: any = {}) {
	        return new CollectionDefaults(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.headers = this.convertValues(source["headers"], collection.KV);
	        this.auth = this.convertValues(source["auth"], collection.Auth);
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
	export class CollectionSummary {
	    root: string;
	    info?: collection.CollectionInfo;
	    active: boolean;
	    readOnly: boolean;
	    linked: boolean;
	    mocking: boolean;
	    mockPort?: number;
	
	    static createFrom(source: any = {}) {
	        return new CollectionSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.root = source["root"];
	        this.info = this.convertValues(source["info"], collection.CollectionInfo);
	        this.active = source["active"];
	        this.readOnly = source["readOnly"];
	        this.linked = source["linked"];
	        this.mocking = source["mocking"];
	        this.mockPort = source["mockPort"];
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
	export class GrpcDefaultInfo {
	    proto: string;
	    imports: string[];
	
	    static createFrom(source: any = {}) {
	        return new GrpcDefaultInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.proto = source["proto"];
	        this.imports = source["imports"];
	    }
	}
	export class GrpcSchemaInfo {
	    proto: string;
	    imports: string[];
	    protos: collection.ProtoFileInfo[];
	    services: proto.Service[];
	
	    static createFrom(source: any = {}) {
	        return new GrpcSchemaInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.proto = source["proto"];
	        this.imports = source["imports"];
	        this.protos = this.convertValues(source["protos"], collection.ProtoFileInfo);
	        this.services = this.convertValues(source["services"], proto.Service);
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
	export class MCPGrant {
	    tool: string;
	    project: string;
	    createdAt: number;
	
	    static createFrom(source: any = {}) {
	        return new MCPGrant(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tool = source["tool"];
	        this.project = source["project"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class MCPStatus {
	    enabled: boolean;
	    running: boolean;
	    addr: string;
	    port: number;
	    url: string;
	    health: string;
	    token: string;
	    readOnly: boolean;
	    origins: string[];
	    groups?: string[];
	    allow?: config.MCPAllowDir[];
	    root: string;
	    projects: number;
	    crossHost: boolean;
	    error: string;
	    hint: string;
	
	    static createFrom(source: any = {}) {
	        return new MCPStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.running = source["running"];
	        this.addr = source["addr"];
	        this.port = source["port"];
	        this.url = source["url"];
	        this.health = source["health"];
	        this.token = source["token"];
	        this.readOnly = source["readOnly"];
	        this.origins = source["origins"];
	        this.groups = source["groups"];
	        this.allow = this.convertValues(source["allow"], config.MCPAllowDir);
	        this.root = source["root"];
	        this.projects = source["projects"];
	        this.crossHost = source["crossHost"];
	        this.error = source["error"];
	        this.hint = source["hint"];
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
	export class ResolveResult {
	    text: string;
	    missing: string[];
	    values: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new ResolveResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.text = source["text"];
	        this.missing = source["missing"];
	        this.values = source["values"];
	    }
	}
	export class ResponseDocInput {
	    uid: string;
	    type: string;
	    params: collection.ResponseParam[];
	    sample: string;
	    contentType?: string;
	    status?: number;
	    headers?: collection.KV[];
	
	    static createFrom(source: any = {}) {
	        return new ResponseDocInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.uid = source["uid"];
	        this.type = source["type"];
	        this.params = this.convertValues(source["params"], collection.ResponseParam);
	        this.sample = source["sample"];
	        this.contentType = source["contentType"];
	        this.status = source["status"];
	        this.headers = this.convertValues(source["headers"], collection.KV);
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
	export class RunCollectionOptions {
	    env_name: string;
	    filter: string;
	    bail: boolean;
	    timeout_sec: number;
	
	    static createFrom(source: any = {}) {
	        return new RunCollectionOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.env_name = source["env_name"];
	        this.filter = source["filter"];
	        this.bail = source["bail"];
	        this.timeout_sec = source["timeout_sec"];
	    }
	}
	export class ServerProject {
	    id: number;
	    title: string;
	
	    static createFrom(source: any = {}) {
	        return new ServerProject(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	    }
	}
	export class SyncBindReq {
	    root: string;
	    serverUrl: string;
	    projectId: number;
	    mode: string;
	    token: string;
	
	    static createFrom(source: any = {}) {
	        return new SyncBindReq(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.root = source["root"];
	        this.serverUrl = source["serverUrl"];
	        this.projectId = source["projectId"];
	        this.mode = source["mode"];
	        this.token = source["token"];
	    }
	}
	export class SyncStatus {
	    linked: boolean;
	    mode: string;
	    running: boolean;
	    lastSyncAt: number;
	    lastError: string;
	    dirtyCount: number;
	    conflicts: number;
	    cursor: number;
	    lastReport?: syncengine.Report;
	
	    static createFrom(source: any = {}) {
	        return new SyncStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.linked = source["linked"];
	        this.mode = source["mode"];
	        this.running = source["running"];
	        this.lastSyncAt = source["lastSyncAt"];
	        this.lastError = source["lastError"];
	        this.dirtyCount = source["dirtyCount"];
	        this.conflicts = source["conflicts"];
	        this.cursor = source["cursor"];
	        this.lastReport = this.convertValues(source["lastReport"], syncengine.Report);
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

export namespace collection {
	
	export class Auth {
	    type?: string;
	    username?: string;
	    password?: string;
	    token?: string;
	    key?: string;
	    value?: string;
	    in?: string;
	
	    static createFrom(source: any = {}) {
	        return new Auth(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.username = source["username"];
	        this.password = source["password"];
	        this.token = source["token"];
	        this.key = source["key"];
	        this.value = source["value"];
	        this.in = source["in"];
	    }
	}
	export class BodyField {
	    name: string;
	    type: string;
	    required: boolean;
	    example?: string;
	    description?: string;
	
	    static createFrom(source: any = {}) {
	        return new BodyField(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.type = source["type"];
	        this.required = source["required"];
	        this.example = source["example"];
	        this.description = source["description"];
	    }
	}
	export class KV {
	    name: string;
	    value: string;
	    enabled: boolean;
	    description?: string;
	    type?: string;
	
	    static createFrom(source: any = {}) {
	        return new KV(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.value = source["value"];
	        this.enabled = source["enabled"];
	        this.description = source["description"];
	        this.type = source["type"];
	    }
	}
	export class Body {
	    type: string;
	    data?: string;
	    raw: string;
	    form: KV[];
	    descs?: Record<string, string>;
	    fields?: BodyField[];
	
	    static createFrom(source: any = {}) {
	        return new Body(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.data = source["data"];
	        this.raw = source["raw"];
	        this.form = this.convertValues(source["form"], KV);
	        this.descs = source["descs"];
	        this.fields = this.convertValues(source["fields"], BodyField);
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
	
	export class EnvVar {
	    uid?: string;
	    name: string;
	    value: string;
	    enabled: boolean;
	    secret: boolean;
	
	    static createFrom(source: any = {}) {
	        return new EnvVar(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.uid = source["uid"];
	        this.name = source["name"];
	        this.value = source["value"];
	        this.enabled = source["enabled"];
	        this.secret = source["secret"];
	    }
	}
	export class Env {
	    uid?: string;
	    name: string;
	    vars: EnvVar[];
	
	    static createFrom(source: any = {}) {
	        return new Env(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.uid = source["uid"];
	        this.name = source["name"];
	        this.vars = this.convertValues(source["vars"], EnvVar);
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
	export class Node {
	    type: string;
	    uid: string;
	    name: string;
	    path: string;
	    method?: string;
	    seq?: number;
	    children?: Node[];
	
	    static createFrom(source: any = {}) {
	        return new Node(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.uid = source["uid"];
	        this.name = source["name"];
	        this.path = source["path"];
	        this.method = source["method"];
	        this.seq = source["seq"];
	        this.children = this.convertValues(source["children"], Node);
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
	export class CollectionInfo {
	    dir: string;
	    name: string;
	    uid: string;
	    tree: Node[];
	    envs: Env[];
	    respUids?: string[];
	
	    static createFrom(source: any = {}) {
	        return new CollectionInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.dir = source["dir"];
	        this.name = source["name"];
	        this.uid = source["uid"];
	        this.tree = this.convertValues(source["tree"], Node);
	        this.envs = this.convertValues(source["envs"], Env);
	        this.respUids = source["respUids"];
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
	export class FieldDiff {
	    field: string;
	    local: string;
	    server: string;
	
	    static createFrom(source: any = {}) {
	        return new FieldDiff(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.field = source["field"];
	        this.local = source["local"];
	        this.server = source["server"];
	    }
	}
	export class ConflictDetail {
	    file: string;
	    kind: string;
	    ofUid: string;
	    name: string;
	    serverRev: number;
	    createdAt: number;
	    mode?: string;
	    diffs: FieldDiff[];
	    hasPayload: boolean;
	    preview?: string;
	    remotePreview?: string;
	
	    static createFrom(source: any = {}) {
	        return new ConflictDetail(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.file = source["file"];
	        this.kind = source["kind"];
	        this.ofUid = source["ofUid"];
	        this.name = source["name"];
	        this.serverRev = source["serverRev"];
	        this.createdAt = source["createdAt"];
	        this.mode = source["mode"];
	        this.diffs = this.convertValues(source["diffs"], FieldDiff);
	        this.hasPayload = source["hasPayload"];
	        this.preview = source["preview"];
	        this.remotePreview = source["remotePreview"];
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
	export class ConflictItem {
	    file: string;
	    kind: string;
	    ofUid: string;
	    name: string;
	    serverRev: number;
	    createdAt: number;
	
	    static createFrom(source: any = {}) {
	        return new ConflictItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.file = source["file"];
	        this.kind = source["kind"];
	        this.ofUid = source["ofUid"];
	        this.name = source["name"];
	        this.serverRev = source["serverRev"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class CreateDocOpts {
	    Name: string;
	    UID: string;
	    Folder: string;
	    OnConflict: string;
	    FileName: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateDocOpts(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Name = source["Name"];
	        this.UID = source["UID"];
	        this.Folder = source["Folder"];
	        this.OnConflict = source["OnConflict"];
	        this.FileName = source["FileName"];
	    }
	}
	export class DocEntry {
	    uid: string;
	    name: string;
	    path: string;
	    content: string;
	    icon?: string;
	    seq?: number;
	
	    static createFrom(source: any = {}) {
	        return new DocEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.uid = source["uid"];
	        this.name = source["name"];
	        this.path = source["path"];
	        this.content = source["content"];
	        this.icon = source["icon"];
	        this.seq = source["seq"];
	    }
	}
	export class DocImageInfo {
	    Hash: string;
	    Size: number;
	    Deduped: boolean;
	
	    static createFrom(source: any = {}) {
	        return new DocImageInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Hash = source["Hash"];
	        this.Size = source["Size"];
	        this.Deduped = source["Deduped"];
	    }
	}
	
	
	export class ExampleRequest {
	    method: string;
	    url: string;
	    headers: KV[];
	    body: Body;
	
	    static createFrom(source: any = {}) {
	        return new ExampleRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.method = source["method"];
	        this.url = source["url"];
	        this.headers = this.convertValues(source["headers"], KV);
	        this.body = this.convertValues(source["body"], Body);
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
	export class ExampleResponse {
	    status: number;
	    proto: string;
	    timeMs: number;
	    size: number;
	    contentType: string;
	    binary: boolean;
	    headers: KV[];
	    body: string;
	
	    static createFrom(source: any = {}) {
	        return new ExampleResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.proto = source["proto"];
	        this.timeMs = source["timeMs"];
	        this.size = source["size"];
	        this.contentType = source["contentType"];
	        this.binary = source["binary"];
	        this.headers = this.convertValues(source["headers"], KV);
	        this.body = source["body"];
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
	
	export class FieldMapping {
	    uid: string;
	    field: string;
	    type: string;
	    description?: string;
	
	    static createFrom(source: any = {}) {
	        return new FieldMapping(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.uid = source["uid"];
	        this.field = source["field"];
	        this.type = source["type"];
	        this.description = source["description"];
	    }
	}
	export class GRPCTLS {
	    mode?: string;
	    ca?: string;
	    cert?: string;
	    key?: string;
	    insecureSkipVerify?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new GRPCTLS(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.ca = source["ca"];
	        this.cert = source["cert"];
	        this.key = source["key"];
	        this.insecureSkipVerify = source["insecureSkipVerify"];
	    }
	}
	export class GRPCBlock {
	    target: string;
	    service: string;
	    method: string;
	    proto?: string;
	    imports?: string[];
	    metadata?: KV[];
	    message?: string;
	    stream?: string;
	    compress?: string;
	    tls?: GRPCTLS;
	
	    static createFrom(source: any = {}) {
	        return new GRPCBlock(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.target = source["target"];
	        this.service = source["service"];
	        this.method = source["method"];
	        this.proto = source["proto"];
	        this.imports = source["imports"];
	        this.metadata = this.convertValues(source["metadata"], KV);
	        this.message = source["message"];
	        this.stream = source["stream"];
	        this.compress = source["compress"];
	        this.tls = this.convertValues(source["tls"], GRPCTLS);
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
	
	export class ImportSummary {
	    imported: number;
	    skipped: number;
	    failures?: string[];
	
	    static createFrom(source: any = {}) {
	        return new ImportSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.imported = source["imported"];
	        this.skipped = source["skipped"];
	        this.failures = source["failures"];
	    }
	}
	
	
	export class ProtoFileInfo {
	    rel: string;
	    size: number;
	    mod: number;
	
	    static createFrom(source: any = {}) {
	        return new ProtoFileInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.rel = source["rel"];
	        this.size = source["size"];
	        this.mod = source["mod"];
	    }
	}
	export class ResponseParam {
	    fieldName: string;
	    type: string;
	    required: boolean;
	    description: string;
	    example: string;
	
	    static createFrom(source: any = {}) {
	        return new ResponseParam(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fieldName = source["fieldName"];
	        this.type = source["type"];
	        this.required = source["required"];
	        this.description = source["description"];
	        this.example = source["example"];
	    }
	}
	export class ResponseDoc {
	    type: string;
	    params: ResponseParam[];
	    sample: string;
	    contentType?: string;
	    status?: number;
	    headers?: KV[];
	
	    static createFrom(source: any = {}) {
	        return new ResponseDoc(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.params = this.convertValues(source["params"], ResponseParam);
	        this.sample = source["sample"];
	        this.contentType = source["contentType"];
	        this.status = source["status"];
	        this.headers = this.convertValues(source["headers"], KV);
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
	export class ScriptAssert {
	    name?: string;
	    expr: string;
	
	    static createFrom(source: any = {}) {
	        return new ScriptAssert(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.expr = source["expr"];
	    }
	}
	export class ScriptBlock {
	    preRequest?: string;
	    postResponse?: string;
	
	    static createFrom(source: any = {}) {
	        return new ScriptBlock(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.preRequest = source["preRequest"];
	        this.postResponse = source["postResponse"];
	    }
	}
	export class ScriptVar {
	    name: string;
	    value: string;
	    enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ScriptVar(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.value = source["value"];
	        this.enabled = source["enabled"];
	    }
	}
	export class RequestSettings {
	    timeoutSec?: number;
	    followRedirects?: boolean;
	    maxRedirects?: number;
	    insecureSsl?: boolean;
	    encodeUrl?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RequestSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.timeoutSec = source["timeoutSec"];
	        this.followRedirects = source["followRedirects"];
	        this.maxRedirects = source["maxRedirects"];
	        this.insecureSsl = source["insecureSsl"];
	        this.encodeUrl = source["encodeUrl"];
	    }
	}
	export class Request {
	    uid: string;
	    name: string;
	    seq: number;
	    path: string;
	    method: string;
	    url: string;
	    summary: string;
	    params: KV[];
	    headers: KV[];
	    body: Body;
	    auth?: Auth;
	    settings?: RequestSettings;
	    grpc?: GRPCBlock;
	    docs: string;
	    baseRev: number;
	    expectHash?: string;
	    varsPreRequest?: ScriptVar[];
	    script?: ScriptBlock;
	    asserts?: ScriptAssert[];
	    respDoc?: ResponseDoc;
	
	    static createFrom(source: any = {}) {
	        return new Request(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.uid = source["uid"];
	        this.name = source["name"];
	        this.seq = source["seq"];
	        this.path = source["path"];
	        this.method = source["method"];
	        this.url = source["url"];
	        this.summary = source["summary"];
	        this.params = this.convertValues(source["params"], KV);
	        this.headers = this.convertValues(source["headers"], KV);
	        this.body = this.convertValues(source["body"], Body);
	        this.auth = this.convertValues(source["auth"], Auth);
	        this.settings = this.convertValues(source["settings"], RequestSettings);
	        this.grpc = this.convertValues(source["grpc"], GRPCBlock);
	        this.docs = source["docs"];
	        this.baseRev = source["baseRev"];
	        this.expectHash = source["expectHash"];
	        this.varsPreRequest = this.convertValues(source["varsPreRequest"], ScriptVar);
	        this.script = this.convertValues(source["script"], ScriptBlock);
	        this.asserts = this.convertValues(source["asserts"], ScriptAssert);
	        this.respDoc = this.convertValues(source["respDoc"], ResponseDoc);
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
	export class RequestCreateOpts {
	    Folder: string;
	    Name: string;
	    Src?: Request;
	    UID: string;
	    OnConflict: string;
	    FileName: string;
	
	    static createFrom(source: any = {}) {
	        return new RequestCreateOpts(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Folder = source["Folder"];
	        this.Name = source["Name"];
	        this.Src = this.convertValues(source["Src"], Request);
	        this.UID = source["UID"];
	        this.OnConflict = source["OnConflict"];
	        this.FileName = source["FileName"];
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
	
	
	export class ResponseExample {
	    uid: string;
	    name: string;
	    requestUid: string;
	    path: string;
	    seq: number;
	    createdAt: number;
	    request: ExampleRequest;
	    response: ExampleResponse;
	
	    static createFrom(source: any = {}) {
	        return new ResponseExample(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.uid = source["uid"];
	        this.name = source["name"];
	        this.requestUid = source["requestUid"];
	        this.path = source["path"];
	        this.seq = source["seq"];
	        this.createdAt = source["createdAt"];
	        this.request = this.convertValues(source["request"], ExampleRequest);
	        this.response = this.convertValues(source["response"], ExampleResponse);
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
	
	
	
	
	export class TrashEntry {
	    file: string;
	    origin: string;
	    kind: string;
	    name: string;
	    deletedAt: number;
	
	    static createFrom(source: any = {}) {
	        return new TrashEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.file = source["file"];
	        this.origin = source["origin"];
	        this.kind = source["kind"];
	        this.name = source["name"];
	        this.deletedAt = source["deletedAt"];
	    }
	}
	export class TrashPreview {
	    kind: string;
	    content: string;
	    files: string[];
	    truncated: boolean;
	
	    static createFrom(source: any = {}) {
	        return new TrashPreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.content = source["content"];
	        this.files = source["files"];
	        this.truncated = source["truncated"];
	    }
	}

}

export namespace config {
	
	export class MCPAllowDir {
	    path: string;
	    writable: boolean;
	
	    static createFrom(source: any = {}) {
	        return new MCPAllowDir(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.writable = source["writable"];
	    }
	}
	export class MCPConfig {
	    enabled: boolean;
	    addr: string;
	    port: number;
	    token: string;
	    readOnly: boolean;
	    allowOrigins: string[];
	    allow?: MCPAllowDir[];
	    interactiveAuth?: boolean;
	    toolGroups?: string[];
	    grants?: mcpauth.Grant[];
	
	    static createFrom(source: any = {}) {
	        return new MCPConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.addr = source["addr"];
	        this.port = source["port"];
	        this.token = source["token"];
	        this.readOnly = source["readOnly"];
	        this.allowOrigins = source["allowOrigins"];
	        this.allow = this.convertValues(source["allow"], MCPAllowDir);
	        this.interactiveAuth = source["interactiveAuth"];
	        this.toolGroups = source["toolGroups"];
	        this.grants = this.convertValues(source["grants"], mcpauth.Grant);
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
	export class Settings {
	    insecureSsl: boolean;
	    timeoutSec: number;
	    followRedirects: boolean;
	    maxRedirects: number;
	    persistCookies: boolean;
	    historyLimit: number;
	    uiScale: number;
	    responseLayout: string;
	    responseSize: number;
	    theme: string;
	    proxyUrl: string;
	    autoSave: boolean;
	    trayResident: boolean;
	    trayCloseAction: string;
	    restoreLimit: number;
	    mcp: MCPConfig;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.insecureSsl = source["insecureSsl"];
	        this.timeoutSec = source["timeoutSec"];
	        this.followRedirects = source["followRedirects"];
	        this.maxRedirects = source["maxRedirects"];
	        this.persistCookies = source["persistCookies"];
	        this.historyLimit = source["historyLimit"];
	        this.uiScale = source["uiScale"];
	        this.responseLayout = source["responseLayout"];
	        this.responseSize = source["responseSize"];
	        this.theme = source["theme"];
	        this.proxyUrl = source["proxyUrl"];
	        this.autoSave = source["autoSave"];
	        this.trayResident = source["trayResident"];
	        this.trayCloseAction = source["trayCloseAction"];
	        this.restoreLimit = source["restoreLimit"];
	        this.mcp = this.convertValues(source["mcp"], MCPConfig);
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

export namespace cookiejar {
	
	export class Info {
	    name: string;
	    value: string;
	    domain: string;
	    path: string;
	    expires: number;
	
	    static createFrom(source: any = {}) {
	        return new Info(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.value = source["value"];
	        this.domain = source["domain"];
	        this.path = source["path"];
	        this.expires = source["expires"];
	    }
	}

}

export namespace history {
	
	export class Entry {
	    time: number;
	    uid: string;
	    name: string;
	    method: string;
	    url: string;
	    status: number;
	    timeMs: number;
	    size: number;
	    error?: string;
	    root?: string;
	    request?: number[];
	    response?: number[];
	
	    static createFrom(source: any = {}) {
	        return new Entry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.time = source["time"];
	        this.uid = source["uid"];
	        this.name = source["name"];
	        this.method = source["method"];
	        this.url = source["url"];
	        this.status = source["status"];
	        this.timeMs = source["timeMs"];
	        this.size = source["size"];
	        this.error = source["error"];
	        this.root = source["root"];
	        this.request = source["request"];
	        this.response = source["response"];
	    }
	}

}

export namespace index {
	
	export class Node {
	    uid: string;
	    type: string;
	    path: string;
	    title: string;
	    method?: string;
	    url?: string;
	    mtime: number;
	    hash?: string;
	    syncedHash?: string;
	    baseRev?: number;
	
	    static createFrom(source: any = {}) {
	        return new Node(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.uid = source["uid"];
	        this.type = source["type"];
	        this.path = source["path"];
	        this.title = source["title"];
	        this.method = source["method"];
	        this.url = source["url"];
	        this.mtime = source["mtime"];
	        this.hash = source["hash"];
	        this.syncedHash = source["syncedHash"];
	        this.baseRev = source["baseRev"];
	    }
	}

}

export namespace mcpaudit {
	
	export class Event {
	    ts: string;
	    tsMs: number;
	    sessionId?: string;
	    transport?: string;
	    client?: string;
	    tool: string;
	    risk: string;
	    project?: string;
	    requestId?: string;
	    approved?: boolean;
	    grantedBy?: string;
	    argsSummary?: string;
	    ok: boolean;
	    resultKind: string;
	    resultSummary?: string;
	    durationMs: number;
	    seq: number;
	
	    static createFrom(source: any = {}) {
	        return new Event(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ts = source["ts"];
	        this.tsMs = source["tsMs"];
	        this.sessionId = source["sessionId"];
	        this.transport = source["transport"];
	        this.client = source["client"];
	        this.tool = source["tool"];
	        this.risk = source["risk"];
	        this.project = source["project"];
	        this.requestId = source["requestId"];
	        this.approved = source["approved"];
	        this.grantedBy = source["grantedBy"];
	        this.argsSummary = source["argsSummary"];
	        this.ok = source["ok"];
	        this.resultKind = source["resultKind"];
	        this.resultSummary = source["resultSummary"];
	        this.durationMs = source["durationMs"];
	        this.seq = source["seq"];
	    }
	}
	export class Page {
	    total: number;
	    events: Event[];
	    file: string;
	    true: boolean;
	    corrupt: number;
	    dropped: number;
	
	    static createFrom(source: any = {}) {
	        return new Page(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.total = source["total"];
	        this.events = this.convertValues(source["events"], Event);
	        this.file = source["file"];
	        this.true = source["true"];
	        this.corrupt = source["corrupt"];
	        this.dropped = source["dropped"];
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

export namespace mcpauth {
	
	export class Grant {
	    tool: string;
	    project: string;
	    createdAt: number;
	
	    static createFrom(source: any = {}) {
	        return new Grant(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tool = source["tool"];
	        this.project = source["project"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class Request {
	    requestId: string;
	    sessionId: string;
	    transport: string;
	    tool: string;
	    risk: string;
	    write: boolean;
	    netEgress: boolean;
	    project: string;
	    argsSummary: string;
	    clientHint: string;
	    createdAt: number;
	
	    static createFrom(source: any = {}) {
	        return new Request(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.requestId = source["requestId"];
	        this.sessionId = source["sessionId"];
	        this.transport = source["transport"];
	        this.tool = source["tool"];
	        this.risk = source["risk"];
	        this.write = source["write"];
	        this.netEgress = source["netEgress"];
	        this.project = source["project"];
	        this.argsSummary = source["argsSummary"];
	        this.clientHint = source["clientHint"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class Result {
	    decision: string;
	    grantedBy: string;
	    reason?: string;
	    requestId?: string;
	
	    static createFrom(source: any = {}) {
	        return new Result(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.decision = source["decision"];
	        this.grantedBy = source["grantedBy"];
	        this.reason = source["reason"];
	        this.requestId = source["requestId"];
	    }
	}

}

export namespace mocksrv {
	
	export class Status {
	    running: boolean;
	    port: number;
	    url: string;
	    hits: number;
	
	    static createFrom(source: any = {}) {
	        return new Status(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.running = source["running"];
	        this.port = source["port"];
	        this.url = source["url"];
	        this.hits = source["hits"];
	    }
	}

}

export namespace proto {
	
	export class FieldInfo {
	    name: string;
	    jsonName: string;
	    type: string;
	    kind: string;
	    repeated: boolean;
	    comment: string;
	
	    static createFrom(source: any = {}) {
	        return new FieldInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.jsonName = source["jsonName"];
	        this.type = source["type"];
	        this.kind = source["kind"];
	        this.repeated = source["repeated"];
	        this.comment = source["comment"];
	    }
	}
	export class Method {
	    name: string;
	    fullName: string;
	    input: string;
	    output: string;
	    comment: string;
	    clientStreaming: boolean;
	    serverStreaming: boolean;
	    stream: string;
	
	    static createFrom(source: any = {}) {
	        return new Method(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.fullName = source["fullName"];
	        this.input = source["input"];
	        this.output = source["output"];
	        this.comment = source["comment"];
	        this.clientStreaming = source["clientStreaming"];
	        this.serverStreaming = source["serverStreaming"];
	        this.stream = source["stream"];
	    }
	}
	export class Service {
	    name: string;
	    comment: string;
	    methods: Method[];
	
	    static createFrom(source: any = {}) {
	        return new Service(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.comment = source["comment"];
	        this.methods = this.convertValues(source["methods"], Method);
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

export namespace runner {
	
	export class Result {
	    url: string;
	    status: number;
	    proto: string;
	    timeMs: number;
	    size: number;
	    sentSize?: number;
	    contentType: string;
	    binary: boolean;
	    headers: collection.KV[];
	    truncated?: boolean;
	    fullSize?: number;
	    warnings?: string[];
	    trailers?: collection.KV[];
	    redirects?: number;
	    remoteAddr?: string;
	    tlsVersion?: string;
	    tlsCipher?: string;
	    reused?: boolean;
	    body: string;
	    script?: any;
	
	    static createFrom(source: any = {}) {
	        return new Result(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.status = source["status"];
	        this.proto = source["proto"];
	        this.timeMs = source["timeMs"];
	        this.size = source["size"];
	        this.sentSize = source["sentSize"];
	        this.contentType = source["contentType"];
	        this.binary = source["binary"];
	        this.headers = this.convertValues(source["headers"], collection.KV);
	        this.truncated = source["truncated"];
	        this.fullSize = source["fullSize"];
	        this.warnings = source["warnings"];
	        this.trailers = this.convertValues(source["trailers"], collection.KV);
	        this.redirects = source["redirects"];
	        this.remoteAddr = source["remoteAddr"];
	        this.tlsVersion = source["tlsVersion"];
	        this.tlsCipher = source["tlsCipher"];
	        this.reused = source["reused"];
	        this.body = source["body"];
	        this.script = source["script"];
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

export namespace script {
	
	export class AssertResult {
	    name: string;
	    expr: string;
	    passed: boolean;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new AssertResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.expr = source["expr"];
	        this.passed = source["passed"];
	        this.error = source["error"];
	    }
	}

}

export namespace syncengine {
	
	export class BindInfo {
	    linked: boolean;
	    serverUrl: string;
	    projectId: number;
	    mode: string;
	    cursor: number;
	    tokenEnc?: string;
	    exampleHashes?: Record<string, string>;
	    docHashes?: Record<string, string>;
	    envHashes?: Record<string, string>;
	    varHashes?: Record<string, string>;
	    fieldMappingHashes?: Record<string, string>;
	    assetUrls?: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new BindInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.linked = source["linked"];
	        this.serverUrl = source["serverUrl"];
	        this.projectId = source["projectId"];
	        this.mode = source["mode"];
	        this.cursor = source["cursor"];
	        this.tokenEnc = source["tokenEnc"];
	        this.exampleHashes = source["exampleHashes"];
	        this.docHashes = source["docHashes"];
	        this.envHashes = source["envHashes"];
	        this.varHashes = source["varHashes"];
	        this.fieldMappingHashes = source["fieldMappingHashes"];
	        this.assetUrls = source["assetUrls"];
	    }
	}
	export class Report {
	    pulled: number;
	    pushed: number;
	    conflicts: number;
	    rejected: number;
	    assets?: number;
	    errors?: string[];
	    cursor: number;
	    gap: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Report(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.pulled = source["pulled"];
	        this.pushed = source["pushed"];
	        this.conflicts = source["conflicts"];
	        this.rejected = source["rejected"];
	        this.assets = source["assets"];
	        this.errors = source["errors"];
	        this.cursor = source["cursor"];
	        this.gap = source["gap"];
	    }
	}

}

export namespace testrun {
	
	export class RunCase {
	    name: string;
	    path: string;
	    uid: string;
	    method: string;
	    url: string;
	    status: number;
	    proto?: string;
	    duration_ms: number;
	    passed: boolean;
	    error?: string;
	    script_error?: string;
	    asserts?: script.AssertResult[];
	
	    static createFrom(source: any = {}) {
	        return new RunCase(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.uid = source["uid"];
	        this.method = source["method"];
	        this.url = source["url"];
	        this.status = source["status"];
	        this.proto = source["proto"];
	        this.duration_ms = source["duration_ms"];
	        this.passed = source["passed"];
	        this.error = source["error"];
	        this.script_error = source["script_error"];
	        this.asserts = this.convertValues(source["asserts"], script.AssertResult);
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
	export class Report {
	    collection: string;
	    env: string;
	    // Go type: time
	    started_at: any;
	    duration_ms: number;
	    total: number;
	    passed: number;
	    failed: number;
	    cases: RunCase[];
	
	    static createFrom(source: any = {}) {
	        return new Report(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.collection = source["collection"];
	        this.env = source["env"];
	        this.started_at = this.convertValues(source["started_at"], null);
	        this.duration_ms = source["duration_ms"];
	        this.total = source["total"];
	        this.passed = source["passed"];
	        this.failed = source["failed"];
	        this.cases = this.convertValues(source["cases"], RunCase);
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

