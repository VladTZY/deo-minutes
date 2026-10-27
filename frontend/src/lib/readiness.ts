import {app} from '../../wailsjs/go/models';
import {t} from './i18n';
import type {Mode} from './status';

// Where a missing item is fixed in Setări.
export type SettingsSection = 'demo' | 'prod' | 'advanced';

export interface Check {
    id: 'openrouter' | 'local' | 'aws' | 'ai';
    ok: boolean;
    label: string;
    detail: string; // what is there, or exactly what to do
    fix: SettingsSection;
    field?: string; // id of the input to focus in Setări
}

// Readiness is what the main page and Setări both read.
export interface Readiness {
    settings: app.Settings | null;
    local: app.LocalStatus | null;
}

export function localReady(local: app.LocalStatus | null): boolean {
    return !!local && local.whisperFound && local.modelFound;
}

// aiReady reports whether the mode's AI (step 3) has what it needs.
export function aiReady(r: Readiness, mode: Mode): boolean {
    const s = r.settings;
    if (!s) {
        return false;
    }
    if (mode === 'demo') {
        return !!s.secrets?.openRouterKey;
    }
    const g = s.gateway;
    const cred = g?.auth === 'sigv4' ? !!s.secrets?.aws : !!s.secrets?.gatewayKey;
    return cred && !!g?.modelId?.trim() && (!!g?.region?.trim() || !!g?.baseUrl?.trim());
}

// checksFor lists what a mode needs before a meeting. null while loading.
export function checksFor(r: Readiness, mode: Mode): Check[] | null {
    if (!r.settings || (mode === 'demo' && !r.local)) {
        return null;
    }
    if (mode === 'demo') {
        const key = aiReady(r, 'demo');
        const local = localReady(r.local);
        return [
            {
                id: 'local', ok: local, fix: 'demo',
                label: t("ready.local"),
                detail: local ? t("ready.local.ok") : t("ready.local.missing"),
            },
            {
                id: 'openrouter', ok: key, fix: 'demo', field: 'or-key',
                label: t("ready.openrouter"),
                detail: key ? t("ready.openrouter.ok") : t("ready.openrouter.missing"),
            },
        ];
    }
    const aws = !!r.settings.secrets?.aws;
    const ai = aiReady(r, 'prod');
    return [
        {
            id: 'aws', ok: aws, fix: 'prod', field: 'aws-id',
            label: t("ready.aws"),
            detail: aws ? t("ready.aws.ok") : t("ready.aws.missing"),
        },
        {
            id: 'ai', ok: ai, fix: 'prod', field: 'gw-model',
            label: t("ready.ai"),
            detail: ai ? t("ready.ai.ok") : t("ready.ai.missing"),
        },
    ];
}
