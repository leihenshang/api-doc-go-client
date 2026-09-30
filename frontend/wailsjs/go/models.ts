export namespace app {
	
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
	
	    static createFrom(source: any = {}) {
	        return new Body(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.data = source["data"];
	        this.raw = source["raw"];
	        this.form = this.convertValues(source["form"], KV);
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
	export class Var {
	    name: string;
	    value: string;
	    enabled: boolean;
	    secret: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Var(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.value = source["value"];
	        this.enabled = source["enabled"];
	        this.secret = source["secret"];
	    }
	}
	export class Env {
	    name: string;
	    vars: Var[];
	
	    static createFrom(source: any = {}) {
	        return new Env(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.vars = this.convertValues(source["vars"], Var);
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
	        this.ofUid = source["ofUid"];
	        this.name = source["name"];
	        this.serverRev = source["serverRev"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class DocEntry {
	    uid: string;
	    name: string;
	    path: string;
	    content: string;
	    icon?: string;
	
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
	    params: KV[];
	    headers: KV[];
	    body: Body;
	    auth?: Auth;
	    settings?: RequestSettings;
	    docs: string;
	    baseRev: number;
	    varsPreRequest?: ScriptVar[];
	    script?: ScriptBlock;
	    asserts?: ScriptAssert[];
	
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
	        this.params = this.convertValues(source["params"], KV);
	        this.headers = this.convertValues(source["headers"], KV);
	        this.body = this.convertValues(source["body"], Body);
	        this.auth = this.convertValues(source["auth"], Auth);
	        this.settings = this.convertValues(source["settings"], RequestSettings);
	        this.docs = source["docs"];
	        this.baseRev = source["baseRev"];
	        this.varsPreRequest = this.convertValues(source["varsPreRequest"], ScriptVar);
	        this.script = this.convertValues(source["script"], ScriptBlock);
	        this.asserts = this.convertValues(source["asserts"], ScriptAssert);
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
	
	
	

}

export namespace config {
	
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
	    request?: number[];
	
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
	        this.request = source["request"];
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

export namespace runner {
	
	export class Result {
	    url: string;
	    status: number;
	    proto: string;
	    timeMs: number;
	    size: number;
	    contentType: string;
	    binary: boolean;
	    headers: collection.KV[];
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
	        this.contentType = source["contentType"];
	        this.binary = source["binary"];
	        this.headers = this.convertValues(source["headers"], collection.KV);
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

export namespace syncengine {
	
	export class BindInfo {
	    linked: boolean;
	    serverUrl: string;
	    projectId: number;
	    mode: string;
	    cursor: number;
	
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
	    }
	}
	export class Report {
	    pulled: number;
	    pushed: number;
	    conflicts: number;
	    rejected: number;
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
	        this.errors = source["errors"];
	        this.cursor = source["cursor"];
	        this.gap = source["gap"];
	    }
	}

}

