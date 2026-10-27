export namespace app {
	
	export class BotSettings {
	    displayName: string;
	    email: string;
	    browserPath: string;
	    lobbyTimeoutMin: number;
	    silenceEndMin: number;
	    headless: boolean;
	
	    static createFrom(source: any = {}) {
	        return new BotSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.displayName = source["displayName"];
	        this.email = source["email"];
	        this.browserPath = source["browserPath"];
	        this.lobbyTimeoutMin = source["lobbyTimeoutMin"];
	        this.silenceEndMin = source["silenceEndMin"];
	        this.headless = source["headless"];
	    }
	}
	export class Counts {
	    actions: number;
	    decisions: number;
	    risks: number;
	    interdependencies: number;
	    changes: number;
	
	    static createFrom(source: any = {}) {
	        return new Counts(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.actions = source["actions"];
	        this.decisions = source["decisions"];
	        this.risks = source["risks"];
	        this.interdependencies = source["interdependencies"];
	        this.changes = source["changes"];
	    }
	}
	export class GatewaySettings {
	    baseUrl: string;
	    region: string;
	    modelId: string;
	    auth: string;
	
	    static createFrom(source: any = {}) {
	        return new GatewaySettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.baseUrl = source["baseUrl"];
	        this.region = source["region"];
	        this.modelId = source["modelId"];
	        this.auth = source["auth"];
	    }
	}
	export class Steps {
	    recorded: boolean;
	    transcribed: boolean;
	    generated: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Steps(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.recorded = source["recorded"];
	        this.transcribed = source["transcribed"];
	        this.generated = source["generated"];
	    }
	}
	export class HistoryItem {
	    id: string;
	    date: string;
	    project: string;
	    meetingNo: number;
	    mode: string;
	    imported: string;
	    excelPath: string;
	    counts: Counts;
	    steps: Steps;
	    canTranscribe: boolean;
	    canGenerate: boolean;
	
	    static createFrom(source: any = {}) {
	        return new HistoryItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.date = source["date"];
	        this.project = source["project"];
	        this.meetingNo = source["meetingNo"];
	        this.mode = source["mode"];
	        this.imported = source["imported"];
	        this.excelPath = source["excelPath"];
	        this.counts = this.convertValues(source["counts"], Counts);
	        this.steps = this.convertValues(source["steps"], Steps);
	        this.canTranscribe = source["canTranscribe"];
	        this.canGenerate = source["canGenerate"];
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
	export class LocalStatus {
	    whisperFound: boolean;
	    modelFound: boolean;
	    vadFound: boolean;
	    modelPath: string;
	
	    static createFrom(source: any = {}) {
	        return new LocalStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.whisperFound = source["whisperFound"];
	        this.modelFound = source["modelFound"];
	        this.vadFound = source["vadFound"];
	        this.modelPath = source["modelPath"];
	    }
	}
	export class OpenRouterSettings {
	    model: string;
	
	    static createFrom(source: any = {}) {
	        return new OpenRouterSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.model = source["model"];
	    }
	}
	export class OutputSettings {
	    dir: string;
	    retentionDays: number;
	    keepAudio: boolean;
	
	    static createFrom(source: any = {}) {
	        return new OutputSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.dir = source["dir"];
	        this.retentionDays = source["retentionDays"];
	        this.keepAudio = source["keepAudio"];
	    }
	}
	export class Result {
	    excelPath: string;
	    transcriptPath: string;
	    folder: string;
	    counts: Counts;
	
	    static createFrom(source: any = {}) {
	        return new Result(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.excelPath = source["excelPath"];
	        this.transcriptPath = source["transcriptPath"];
	        this.folder = source["folder"];
	        this.counts = this.convertValues(source["counts"], Counts);
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
	export class SecretStatus {
	    openRouterKey: boolean;
	    gatewayKey: boolean;
	    aws: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SecretStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.openRouterKey = source["openRouterKey"];
	        this.gatewayKey = source["gatewayKey"];
	        this.aws = source["aws"];
	    }
	}
	export class TranscribeSettings {
	    region: string;
	    multiLanguage: boolean;
	
	    static createFrom(source: any = {}) {
	        return new TranscribeSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.region = source["region"];
	        this.multiLanguage = source["multiLanguage"];
	    }
	}
	export class Settings {
	    mode: string;
	    openrouter: OpenRouterSettings;
	    gateway: GatewaySettings;
	    transcribe: TranscribeSettings;
	    bot: BotSettings;
	    output: OutputSettings;
	    secrets: SecretStatus;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.openrouter = this.convertValues(source["openrouter"], OpenRouterSettings);
	        this.gateway = this.convertValues(source["gateway"], GatewaySettings);
	        this.transcribe = this.convertValues(source["transcribe"], TranscribeSettings);
	        this.bot = this.convertValues(source["bot"], BotSettings);
	        this.output = this.convertValues(source["output"], OutputSettings);
	        this.secrets = this.convertValues(source["secrets"], SecretStatus);
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
	export class StartDefaults {
	    project: string;
	    meetingNo: number;
	    preparedBy: string;
	
	    static createFrom(source: any = {}) {
	        return new StartDefaults(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.project = source["project"];
	        this.meetingNo = source["meetingNo"];
	        this.preparedBy = source["preparedBy"];
	    }
	}
	export class StartRequest {
	    link: string;
	    password: string;
	    project: string;
	    meetingNo: number;
	    preparedBy: string;
	
	    static createFrom(source: any = {}) {
	        return new StartRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.link = source["link"];
	        this.password = source["password"];
	        this.project = source["project"];
	        this.meetingNo = source["meetingNo"];
	        this.preparedBy = source["preparedBy"];
	    }
	}
	export class Status {
	    state: string;
	    sessionId: string;
	    mode: string;
	    progress: number;
	    steps: Steps;
	    label: string;
	    startedAt: number;
	    participants: number;
	    error: string;
	    warnings: string[];
	    result?: Result;
	    canStop: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Status(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.state = source["state"];
	        this.sessionId = source["sessionId"];
	        this.mode = source["mode"];
	        this.progress = source["progress"];
	        this.steps = this.convertValues(source["steps"], Steps);
	        this.label = source["label"];
	        this.startedAt = source["startedAt"];
	        this.participants = source["participants"];
	        this.error = source["error"];
	        this.warnings = source["warnings"];
	        this.result = this.convertValues(source["result"], Result);
	        this.canStop = source["canStop"];
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

