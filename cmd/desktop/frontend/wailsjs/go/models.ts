export namespace main {
	
	export class MentorMessage {
	    id: string;
	    content: string;
	    createdAt: string;
	
	    static createFrom(source: any = {}) {
	        return new MentorMessage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.content = source["content"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class SessionInfo {
	    conversationId: string;
	
	    static createFrom(source: any = {}) {
	        return new SessionInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.conversationId = source["conversationId"];
	    }
	}

}

