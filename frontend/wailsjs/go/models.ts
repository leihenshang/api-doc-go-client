export namespace app {
	
	export class ResolveResult {
	    text: string;
	    missing: string[];
	
	    static createFrom(source: any = {}) {
	        return new ResolveResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.text = source["text"];
	        this.missing = source["missing"];
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
	
	    static createFrom(source: any = {}) {
	        return new KV(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.value = source["value"];
	        this.enabled = source["enabled"];
	        this.description = source["description"];
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

