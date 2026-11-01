export namespace app {
	
	export class BackendConnectionInfo {
	    baseUrl: string;
	    overridden: boolean;
	
	    static createFrom(source: any = {}) {
	        return new BackendConnectionInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.baseUrl = source["baseUrl"];
	        this.overridden = source["overridden"];
	    }
	}
	export class ConnectionInfo {
	    proto: string;
	    local: string;
	    remote: string;
	    state: string;
	
	    static createFrom(source: any = {}) {
	        return new ConnectionInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.proto = source["proto"];
	        this.local = source["local"];
	        this.remote = source["remote"];
	        this.state = source["state"];
	    }
	}
	export class FileInfo {
	    name: string;
	    path: string;
	    isDir: boolean;
	    size: number;
	    modifiedTime: number;
	
	    static createFrom(source: any = {}) {
	        return new FileInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.isDir = source["isDir"];
	        this.size = source["size"];
	        this.modifiedTime = source["modifiedTime"];
	    }
	}
	export class GoroutineInfo {
	    id: string;
	    state: string;
	    stack: string;
	
	    static createFrom(source: any = {}) {
	        return new GoroutineInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.state = source["state"];
	        this.stack = source["stack"];
	    }
	}
	export class MemoryDetails {
	    alloc: number;
	    totalAlloc: number;
	    sys: number;
	    numGC: number;
	    nextGC: number;
	    pauseTotalNs: number;
	
	    static createFrom(source: any = {}) {
	        return new MemoryDetails(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.alloc = source["alloc"];
	        this.totalAlloc = source["totalAlloc"];
	        this.sys = source["sys"];
	        this.numGC = source["numGC"];
	        this.nextGC = source["nextGC"];
	        this.pauseTotalNs = source["pauseTotalNs"];
	    }
	}
	export class TraceAllSpansResponse {
	    spans: tracer.Span[];
	    truncated: boolean;
	
	    static createFrom(source: any = {}) {
	        return new TraceAllSpansResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.spans = this.convertValues(source["spans"], tracer.Span);
	        this.truncated = source["truncated"];
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
	export class TraceConfigResponse {
	    traceSamplingThreshold: number;
	    traceMaxAgeDays: number;
	    traceMaxRows: number;
	    traceVacuumThresholdMB: number;
	
	    static createFrom(source: any = {}) {
	        return new TraceConfigResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.traceSamplingThreshold = source["traceSamplingThreshold"];
	        this.traceMaxAgeDays = source["traceMaxAgeDays"];
	        this.traceMaxRows = source["traceMaxRows"];
	        this.traceVacuumThresholdMB = source["traceVacuumThresholdMB"];
	    }
	}
	export class TraceListResponse {
	    total: number;
	    page: number;
	    size: number;
	    items: tracer.TraceSummary[];
	
	    static createFrom(source: any = {}) {
	        return new TraceListResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.total = source["total"];
	        this.page = source["page"];
	        this.size = source["size"];
	        this.items = this.convertValues(source["items"], tracer.TraceSummary);
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
	export class TraceModulesResponse {
	    global: boolean;
	    modules: tracer.TraceModule[];
	
	    static createFrom(source: any = {}) {
	        return new TraceModulesResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.global = source["global"];
	        this.modules = this.convertValues(source["modules"], tracer.TraceModule);
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
	
	export class ConfigFieldMeta {
	    key: string;
	    label: string;
	    type: string;
	    defaultValue: any;
	    description: string;
	    group: string;
	    groupOrder: number;
	
	    static createFrom(source: any = {}) {
	        return new ConfigFieldMeta(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.label = source["label"];
	        this.type = source["type"];
	        this.defaultValue = source["defaultValue"];
	        this.description = source["description"];
	        this.group = source["group"];
	        this.groupOrder = source["groupOrder"];
	    }
	}

}

export namespace preselect {
	
	export class DirEntry {
	    path: string;
	    fileCount: number;
	
	    static createFrom(source: any = {}) {
	        return new DirEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.fileCount = source["fileCount"];
	    }
	}
	export class Result {
	    dirs: DirEntry[];
	    files: string[];
	    missing: string[];
	    received: number;
	
	    static createFrom(source: any = {}) {
	        return new Result(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.dirs = this.convertValues(source["dirs"], DirEntry);
	        this.files = source["files"];
	        this.missing = source["missing"];
	        this.received = source["received"];
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

export namespace server {
	
	export class AppLogFileInfo {
	    name: string;
	    date: string;
	    size: number;
	    modifiedMs: number;
	
	    static createFrom(source: any = {}) {
	        return new AppLogFileInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.date = source["date"];
	        this.size = source["size"];
	        this.modifiedMs = source["modifiedMs"];
	    }
	}
	export class AppLogPage {
	    name: string;
	    totalSize: number;
	    returnedLines: number;
	    hasMore: boolean;
	    nextOffset: number;
	    lines: string[];
	
	    static createFrom(source: any = {}) {
	        return new AppLogPage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.totalSize = source["totalSize"];
	        this.returnedLines = source["returnedLines"];
	        this.hasMore = source["hasMore"];
	        this.nextOffset = source["nextOffset"];
	        this.lines = source["lines"];
	    }
	}
	export class HTTPRequestLog {
	    id: number;
	    timestamp: string;
	    timestampMs: number;
	    method: string;
	    path: string;
	    query?: string;
	    remoteAddr: string;
	    requestHeaders: Record<string, Array<string>>;
	    requestBody?: string;
	    requestBodyTruncated?: boolean;
	    statusCode: number;
	    responseHeaders: Record<string, Array<string>>;
	    responseBody?: string;
	    responseBodyTruncated?: boolean;
	    responseBytes: number;
	    durationMs: number;
	    direction: string;
	    peer: string;
	    taskId?: string;
	
	    static createFrom(source: any = {}) {
	        return new HTTPRequestLog(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.timestamp = source["timestamp"];
	        this.timestampMs = source["timestampMs"];
	        this.method = source["method"];
	        this.path = source["path"];
	        this.query = source["query"];
	        this.remoteAddr = source["remoteAddr"];
	        this.requestHeaders = source["requestHeaders"];
	        this.requestBody = source["requestBody"];
	        this.requestBodyTruncated = source["requestBodyTruncated"];
	        this.statusCode = source["statusCode"];
	        this.responseHeaders = source["responseHeaders"];
	        this.responseBody = source["responseBody"];
	        this.responseBodyTruncated = source["responseBodyTruncated"];
	        this.responseBytes = source["responseBytes"];
	        this.durationMs = source["durationMs"];
	        this.direction = source["direction"];
	        this.peer = source["peer"];
	        this.taskId = source["taskId"];
	    }
	}
	export class HTTPLogPage {
	    total: number;
	    page: number;
	    size: number;
	    entries: HTTPRequestLog[];
	
	    static createFrom(source: any = {}) {
	        return new HTTPLogPage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.total = source["total"];
	        this.page = source["page"];
	        this.size = source["size"];
	        this.entries = this.convertValues(source["entries"], HTTPRequestLog);
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
	export class HTTPLogQuery {
	    page: number;
	    size: number;
	    method: string;
	    pathLike: string;
	    statusGroup: string;
	    startMs: number;
	    endMs: number;
	    excludePing: boolean;
	
	    static createFrom(source: any = {}) {
	        return new HTTPLogQuery(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.page = source["page"];
	        this.size = source["size"];
	        this.method = source["method"];
	        this.pathLike = source["pathLike"];
	        this.statusGroup = source["statusGroup"];
	        this.startMs = source["startMs"];
	        this.endMs = source["endMs"];
	        this.excludePing = source["excludePing"];
	    }
	}
	export class HTTPLogStats {
	    total: number;
	    dbPath: string;
	    dbSize: number;
	    droppedMem: number;
	
	    static createFrom(source: any = {}) {
	        return new HTTPLogStats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.total = source["total"];
	        this.dbPath = source["dbPath"];
	        this.dbSize = source["dbSize"];
	        this.droppedMem = source["droppedMem"];
	    }
	}

}

export namespace tracer {
	
	export class Span {
	    id?: number;
	    spanId: string;
	    parentSpanId: string;
	    traceId: string;
	    taskId: string;
	    runSeq: number;
	    traceType: string;
	    stage: string;
	    filename?: string;
	    startMs: number;
	    endMs: number;
	    durationMs: number;
	    success: boolean;
	    errorMessage?: string;
	    extra?: string;
	    createdAtMs: number;
	
	    static createFrom(source: any = {}) {
	        return new Span(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.spanId = source["spanId"];
	        this.parentSpanId = source["parentSpanId"];
	        this.traceId = source["traceId"];
	        this.taskId = source["taskId"];
	        this.runSeq = source["runSeq"];
	        this.traceType = source["traceType"];
	        this.stage = source["stage"];
	        this.filename = source["filename"];
	        this.startMs = source["startMs"];
	        this.endMs = source["endMs"];
	        this.durationMs = source["durationMs"];
	        this.success = source["success"];
	        this.errorMessage = source["errorMessage"];
	        this.extra = source["extra"];
	        this.createdAtMs = source["createdAtMs"];
	    }
	}
	export class TraceModule {
	    code: string;
	    name: string;
	    description: string;
	    enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new TraceModule(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.enabled = source["enabled"];
	    }
	}
	export class TraceSummary {
	    traceId: string;
	    taskId?: string;
	    runSeq: number;
	    traceType: string;
	    taskName?: string;
	    startMs: number;
	    endMs: number;
	    durationMs: number;
	    spanCount: number;
	    hasFailure: boolean;
	    totalSelfDurationMs: number;
	
	    static createFrom(source: any = {}) {
	        return new TraceSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.traceId = source["traceId"];
	        this.taskId = source["taskId"];
	        this.runSeq = source["runSeq"];
	        this.traceType = source["traceType"];
	        this.taskName = source["taskName"];
	        this.startMs = source["startMs"];
	        this.endMs = source["endMs"];
	        this.durationMs = source["durationMs"];
	        this.spanCount = source["spanCount"];
	        this.hasFailure = source["hasFailure"];
	        this.totalSelfDurationMs = source["totalSelfDurationMs"];
	    }
	}

}

export namespace uploader {
	
	export class FileProgress {
	    fileName: string;
	    relativePath: string;
	    absolutePath?: string;
	    status: string;
	    totalSize: number;
	    uploadedSize: number;
	    progress: number;
	
	    static createFrom(source: any = {}) {
	        return new FileProgress(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fileName = source["fileName"];
	        this.relativePath = source["relativePath"];
	        this.absolutePath = source["absolutePath"];
	        this.status = source["status"];
	        this.totalSize = source["totalSize"];
	        this.uploadedSize = source["uploadedSize"];
	        this.progress = source["progress"];
	    }
	}
	export class PagedFileResult {
	    files: FileProgress[];
	    total: number;
	
	    static createFrom(source: any = {}) {
	        return new PagedFileResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.files = this.convertValues(source["files"], FileProgress);
	        this.total = source["total"];
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
	export class TaskProgress {
	    taskId: string;
	    taskName: string;
	    status: string;
	    totalSize: number;
	    uploadedSize: number;
	    totalFiles: number;
	    completedFiles: number;
	    failedFiles: number;
	    progress: number;
	    files: FileProgress[];
	    traceId?: string;
	
	    static createFrom(source: any = {}) {
	        return new TaskProgress(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.taskId = source["taskId"];
	        this.taskName = source["taskName"];
	        this.status = source["status"];
	        this.totalSize = source["totalSize"];
	        this.uploadedSize = source["uploadedSize"];
	        this.totalFiles = source["totalFiles"];
	        this.completedFiles = source["completedFiles"];
	        this.failedFiles = source["failedFiles"];
	        this.progress = source["progress"];
	        this.files = this.convertValues(source["files"], FileProgress);
	        this.traceId = source["traceId"];
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

