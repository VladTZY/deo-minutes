import {app} from '../../wailsjs/go/models';

export const IDLE_STATUS = {
    state: 'idle',
    label: '',
    startedAt: 0,
    participants: 0,
    error: '',
    warnings: [],
    canStop: false,
} as unknown as app.Status;

const ACTIVE = new Set([
    'starting_browser',
    'connecting',
    'lobby',
    'recording',
    'finishing_transcript',
    'generating',
    'saving',
]);

// isActive reports whether a session (live or resumed) is in progress.
export function isActive(state: string | undefined): boolean {
    return state !== undefined && ACTIVE.has(state);
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

// Pipeline phases shown as a progress list. Index of the current phase, -1 if none.
export const PHASES = ['connecting', 'recording', 'finishing_transcript', 'generating', 'saving'] as const;

export function phaseIndex(state: string): number {
    switch (state) {
        case 'starting_browser':
        case 'connecting':
        case 'lobby':
            return 0;
        case 'recording':
            return 1;
        case 'finishing_transcript':
            return 2;
        case 'generating':
            return 3;
        case 'saving':
            return 4;
        case 'done':
            return PHASES.length;
        default:
            return -1;
    }
}

export function formatDuration(ms: number): string {
    const total = Math.max(0, Math.floor(ms / 1000));
    const h = Math.floor(total / 3600);
    const m = Math.floor((total % 3600) / 60);
    const s = total % 60;
    const pad = (n: number) => String(n).padStart(2, '0');
    return `${pad(h)}:${pad(m)}:${pad(s)}`;
}

// A transcript event line looks like "[HH:MM:SS] Nume: text".
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
