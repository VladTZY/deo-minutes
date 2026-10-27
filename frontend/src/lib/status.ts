import {app} from '../../wailsjs/go/models';

export type Mode = 'demo' | 'prod';

export const IDLE_STATUS = {
    state: 'idle',
    sessionId: '',
    mode: '',
    progress: 0,
    steps: {recorded: false, transcribed: false, generated: false},
    label: '',
    startedAt: 0,
    participants: 0,
    error: '',
    warnings: [],
    canStop: false,
} as unknown as app.Status;

// States in which the backend is busy with a session (live or a step).
const ACTIVE = new Set([
    'starting_browser',
    'connecting',
    'lobby',
    'recording',
    'finishing_transcript',
    'transcribing',
    'generating',
    'saving',
]);

export function isActive(state: string | undefined): boolean {
    return state !== undefined && ACTIVE.has(state);
}

// Meeting phase: the bot is joining or in the meeting.
const LIVE = new Set(['starting_browser', 'connecting', 'lobby', 'recording', 'finishing_transcript']);

export function isLive(state: string): boolean {
    return LIVE.has(state);
}

// Visual tone of a state: drives badge colour only.
export type Tone = 'idle' | 'joining' | 'recording' | 'processing' | 'done' | 'error';

export function toneOf(state: string): Tone {
    switch (state) {
        case 'starting_browser':
        case 'connecting':
        case 'lobby':
            return 'joining';
        case 'recording':
            return 'recording';
        case 'finishing_transcript':
        case 'transcribing':
        case 'generating':
        case 'saving':
            return 'processing';
        case 'done':
            return 'done';
        case 'error':
            return 'error';
        default:
            return 'idle';
    }
}

// ---------- The 3-step flow ----------

// done: finished. current: running now. next: waits for the user's click.
// auto: will run by itself (PROD). waiting: not reached yet. error: failed here.
export type StepState = 'done' | 'current' | 'next' | 'auto' | 'waiting' | 'error';
export type StepIndex = 0 | 1 | 2;
export type Flow = [StepState, StepState, StepState];

// stepOfState tells which step a (non-error) state belongs to.
export function stepOfState(state: string): StepIndex | -1 {
    switch (state) {
        case 'starting_browser':
        case 'connecting':
        case 'lobby':
        case 'recording':
            return 0;
        case 'finishing_transcript':
        case 'recorded':
        case 'transcribing':
            return 1;
        case 'transcribed':
        case 'generating':
        case 'saving':
            return 2;
        default:
            return -1;
    }
}

// flowOf derives the three step marks. failedAt is the step the session was
// in when it failed (the backend does not refresh Steps during a PROD run).
export function flowOf(status: app.Status, setup: boolean, mode: Mode, failedAt: StepIndex | -1): Flow {
    const auto: StepState = mode === 'prod' ? 'auto' : 'waiting';
    if (setup) {
        return ['next', auto, auto];
    }
    const prod = (status.mode || mode) === 'prod';
    const later: StepState = prod ? 'auto' : 'waiting';
    switch (status.state) {
        case 'starting_browser':
        case 'connecting':
        case 'lobby':
            return ['current', later, later];
        case 'recording':
            return ['current', prod ? 'current' : 'waiting', later];
        case 'finishing_transcript':
            return ['done', 'current', later];
        case 'recorded':
            return ['done', 'next', 'waiting'];
        case 'transcribing':
            return ['done', 'current', 'waiting'];
        case 'transcribed':
            return ['done', 'done', 'next'];
        case 'generating':
        case 'saving':
            return ['done', 'done', 'current'];
        case 'done':
            return ['done', 'done', 'done'];
        case 'error': {
            const s = status.steps;
            let at: number = failedAt;
            if (at < 0) {
                at = s?.transcribed ? 2 : s?.recorded ? 1 : 0;
            }
            return [0, 1, 2].map(i => (i < at ? 'done' : i === at ? 'error' : 'waiting')) as Flow;
        }
        default:
            return ['next', auto, auto];
    }
}

// ---------- Formatting ----------

export function formatDuration(ms: number): string {
    const total = Math.max(0, Math.floor(ms / 1000));
    const h = Math.floor(total / 3600);
    const m = Math.floor((total % 3600) / 60);
    const s = total % 60;
    const pad = (n: number) => String(n).padStart(2, '0');
    return `${pad(h)}:${pad(m)}:${pad(s)}`;
}

// A transcript line looks like "[HH:MM:SS] Nume: text".
export interface TranscriptLine {
    time: string;
    speaker: string;
    text: string;
}

const LINE_RE = /^\[(\d{2}:\d{2}:\d{2})]\s+([^:]+):\s?(.*)$/s;

export function parseLine(raw: string): TranscriptLine {
    const m = LINE_RE.exec(raw);
    if (!m) {
        return {time: '', speaker: '', text: raw};
    }
    return {time: m[1], speaker: m[2].trim(), text: m[3]};
}
