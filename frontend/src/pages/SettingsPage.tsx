import {ReactNode, SyntheticEvent, useEffect, useRef, useState} from 'react';
import {
    ChooseOutputDir,
    DeleteAWSCredentials,
    DeleteGatewayKey,
    DeleteOpenRouterKey,
    GetSettings,
    LocalStatus,
    SaveSettings,
    SetAWSCredentials,
    SetGatewayKey,
    SetOpenRouterKey,
    TestAI,
    TestTranscribe,
    Version,
} from '../../wailsjs/go/app/App';
import {app} from '../../wailsjs/go/models';
import {t} from '../lib/i18n';
import {errorText} from '../lib/errors';
import type {Mode} from '../lib/status';
import {aiReady, localReady, SettingsSection} from '../lib/readiness';
import type {SettingsFocus} from '../App';

type Msg = { ok: boolean; text: string } | null;

// Editable part of the settings. Mode is owned by the header switch and
// secrets are tracked separately, so neither overwrites unsaved edits.
type Form = Pick<app.Settings, 'openrouter' | 'gateway' | 'transcribe' | 'bot' | 'output'>;

const DEFAULT_MODEL = 'anthropic/claude-opus-5.5';

function toForm(s: app.Settings): Form {
    return {
        openrouter: {...s.openrouter},
        gateway: {...s.gateway},
        transcribe: {...s.transcribe},
        bot: {...s.bot},
        output: {...s.output},
    };
}

interface Props {
    mode: Mode;
    focus: SettingsFocus | null;
    onChanged: () => void;
}

const NO_SECRETS = {openRouterKey: false, gatewayKey: false, aws: false} as app.SecretStatus;

export default function SettingsPage({mode, focus, onChanged}: Props) {
    const [form, setForm] = useState<Form | null>(null);
    const [saved, setSaved] = useState('');
    const [secrets, setSecrets] = useState<app.SecretStatus>(NO_SECRETS);
    const [local, setLocal] = useState<app.LocalStatus | null>(null);
    const [loadError, setLoadError] = useState('');
    const [version, setVersion] = useState('');
    const [open, setOpen] = useState<Record<SettingsSection, boolean>>({
        demo: mode === 'demo',
        prod: mode === 'prod',
        advanced: false,
    });

    const [busy, setBusy] = useState<string | null>(null);
    const [msgs, setMsgs] = useState<Record<string, Msg>>({});

    // Write-only secret inputs: never filled from the backend, cleared after use.
    const [orKey, setOrKey] = useState('');
    const [gwKey, setGwKey] = useState('');
    const [awsId, setAwsId] = useState('');
    const [awsSecret, setAwsSecret] = useState('');
    const [awsToken, setAwsToken] = useState('');

    const sectionRefs = {
        demo: useRef<HTMLDetailsElement>(null),
        prod: useRef<HTMLDetailsElement>(null),
        advanced: useRef<HTMLDetailsElement>(null),
    };

    useEffect(() => {
        GetSettings()
            .then(s => {
                const f = toForm(s);
                setForm(f);
                setSaved(JSON.stringify(f));
                setSecrets(s.secrets ?? NO_SECRETS);
            })
            .catch(e => setLoadError(errorText(e)));
        LocalStatus().then(setLocal).catch(() => undefined);
        Version().then(setVersion).catch(() => undefined);
    }, []);

    // The current mode's section is the open one.
    useEffect(() => {
        setOpen(o => ({...o, demo: mode === 'demo', prod: mode === 'prod'}));
    }, [mode]);

    // "Deschide Setări" from a readiness check: open and show that section.
    useEffect(() => {
        if (!focus) {
            return;
        }
        setOpen(o => ({...o, [focus.section]: true}));
        LocalStatus().then(setLocal).catch(() => undefined);
        const id = window.setTimeout(() => {
            const el = sectionRefs[focus.section].current;
            const field = focus.field ? document.getElementById(focus.field) : null;
            if (field) {
                field.scrollIntoView({block: 'center'});
                field.focus({preventScroll: true});
            } else {
                el?.scrollIntoView({block: 'start'});
                el?.querySelector<HTMLElement>('summary')?.focus({preventScroll: true});
            }
        }, 0);
        return () => window.clearTimeout(id);
    }, [focus]); // sectionRefs are stable

    if (loadError) {
        return <div className="page"><p className="msg msg-err" role="alert">{loadError}</p></div>;
    }
    if (!form) {
        return <div className="page"><p className="muted" role="status">{t("settings.loading")}</p></div>;
    }

    const dirty = JSON.stringify(form) !== saved;
    const setMsg = (k: string, m: Msg) => setMsgs(x => ({...x, [k]: m}));

    function patch<K extends keyof Form>(section: K, values: Partial<Form[K]>) {
        setMsg('save', null);
        setForm(f => (f ? {...f, [section]: {...f[section], ...values}} : f));
    }

    async function refreshSecrets() {
        try {
            const s = await GetSettings();
            setSecrets(s.secrets);
        } catch {
            // badge stays as it was
        }
        onChanged();
    }

    // persist saves the current form; returns an error text or '' on success.
    async function persist(): Promise<string> {
        if (!form) {
            return '';
        }
        try {
            await SaveSettings(app.Settings.createFrom({...form, mode, secrets}));
            setSaved(JSON.stringify(form));
            onChanged();
            return '';
        } catch (e) {
            return errorText(e);
        }
    }

    async function run(name: string, fn: () => Promise<void>) {
        setBusy(name);
        try {
            await fn();
        } finally {
            setBusy(null);
        }
    }

    const onSave = () => run('save', async () => {
        setMsg('save', null);
        const err = await persist();
        setMsg('save', err ? {ok: false, text: err} : {ok: true, text: t("settings.saved")});
    });

    // Tests read the saved configuration, so pending edits are saved first.
    const onTest = (which: 'ai-demo' | 'ai-prod' | 'transcribe') => run(which, async () => {
        setMsg(which, null);
        if (dirty) {
            const err = await persist();
            if (err) {
                setMsg(which, {ok: false, text: err});
                return;
            }
        }
        try {
            await (which === 'transcribe' ? TestTranscribe() : TestAI());
            setMsg(which, {ok: true, text: t("settings.test.ok")});
        } catch (e) {
            setMsg(which, {ok: false, text: errorText(e)});
        }
    });

    const secretAction = (name: string, fn: () => Promise<void>, okText: string, clear: () => void) => run(name, async () => {
        setMsg(name, null);
        try {
            await fn();
            setMsg(name, {ok: true, text: okText});
        } catch (e) {
            setMsg(name, {ok: false, text: errorText(e)});
        } finally {
            clear();
            await refreshSecrets();
        }
    });

    const onChooseDir = () => run('dir', async () => {
        setMsg('dir', null);
        try {
            const dir = await ChooseOutputDir();
            if (dir) {
                patch('output', {dir});
            }
        } catch (e) {
            setMsg('dir', {ok: false, text: errorText(e)});
        }
    });

    const num = (v: string) => Number.parseInt(v, 10) || 0;
    const locked = busy !== null;
    const toggle = (s: SettingsSection) => (e: SyntheticEvent<HTMLDetailsElement>) => {
        const isOpen = e.currentTarget.open;
        setOpen(o => (o[s] === isOpen ? o : {...o, [s]: isOpen}));
    };

    const r = {settings: app.Settings.createFrom({...form, mode, secrets}), local};
    const demoOk = aiReady(r, 'demo') && localReady(local);
    const prodOk = !!secrets.aws && aiReady(r, 'prod');
    const usesKey = form.gateway.auth !== 'sigv4';

    return (
        <div className="page settings">
            <h1 className="page-title">{t("settings.title")}</h1>
            <p className="page-lead">{t("settings.lead")}</p>

            {/* ---------- DEMO ---------- */}
            <details ref={sectionRefs.demo} className="panel group"
                     open={open.demo} onToggle={toggle('demo')}>
                <summary className="group-head">
                    <span className="group-title">{t("settings.demo.title")}</span>
                    {mode === 'demo' && <span className="mode-tag mode-tag-demo">{t("settings.activeMode")}</span>}
                    <ReadyBadge ok={demoOk}/>
                    <span className="group-desc">{t("mode.demo.desc")}</span>
                </summary>
                <div className="group-body">
                    <Block title={t("settings.openrouter.title")} badge={<SecretBadge set={secrets.openRouterKey}/>}>
                        <SecretInput
                            id="or-key"
                            label={t("settings.openrouter.key")}
                            hint={t("settings.openrouter.key.hint")}
                            value={orKey}
                            onChange={setOrKey}
                            locked={locked}
                            isSet={secrets.openRouterKey}
                            msg={msgs['or-key']}
                            onSave={() => secretAction('or-key', () => SetOpenRouterKey(orKey), t("settings.saved"), () => setOrKey(''))}
                            onDelete={() => secretAction('or-key', () => DeleteOpenRouterKey(), t("settings.secret.deleted"), () => undefined)}
                        />
                        <div className="field">
                            <label htmlFor="or-model">{t("settings.openrouter.model")}</label>
                            <input
                                id="or-model"
                                name="or-model"
                                type="text"
                                spellCheck={false}
                                autoComplete="off"
                                placeholder={DEFAULT_MODEL}
                                aria-describedby="or-model-hint"
                                value={form.openrouter.model}
                                onChange={e => patch('openrouter', {model: e.target.value})}
                            />
                            <p id="or-model-hint" className="hint">{t("settings.openrouter.model.hint", DEFAULT_MODEL)}</p>
                        </div>
                        <TestRow
                            label={busy === 'ai-demo' ? t("settings.testing") : t("settings.test.openrouter")}
                            disabled={locked || mode !== 'demo'}
                            note={mode !== 'demo' ? t("settings.test.onlyDemo") : ''}
                            msg={msgs['ai-demo']}
                            onClick={() => onTest('ai-demo')}
                        />
                    </Block>

                    <Block title={t("settings.local.title")} badge={
                        <span className={localReady(local) ? 'badge badge-ok' : 'badge badge-warn'}>
                            {localReady(local) ? t("ready.local.ok") : t("settings.local.missing")}
                        </span>
                    }>
                        {local === null ? (
                            <p className="muted" role="status">{t("ready.loading")}</p>
                        ) : (
                            <>
                                <ul className="checks checks-compact">
                                    <LocalItem ok={local.whisperFound} label={t("settings.local.whisper")}/>
                                    <LocalItem ok={local.modelFound} label={t("settings.local.model")}/>
                                    <LocalItem ok={local.vadFound} label={t("settings.local.vad")} optional/>
                                </ul>
                                {local.modelPath && (
                                    <p className="hint">{t("settings.local.path")} <code className="path">{local.modelPath}</code></p>
                                )}
                                {!localReady(local) && <p className="alert alert-warn">{t("err.local.missing")}</p>}
                            </>
                        )}
                    </Block>
                </div>
            </details>

            {/* ---------- PROD ---------- */}
            <details ref={sectionRefs.prod} className="panel group"
                     open={open.prod} onToggle={toggle('prod')}>
                <summary className="group-head">
                    <span className="group-title">{t("settings.prod.title")}</span>
                    {mode === 'prod' && <span className="mode-tag mode-tag-prod">{t("settings.activeMode")}</span>}
                    <ReadyBadge ok={prodOk}/>
                    <span className="group-desc">{t("mode.prod.desc")}</span>
                </summary>
                <div className="group-body">
                    <Block title={t("settings.section.aws")} badge={<SecretBadge set={secrets.aws}/>}>
                        <p className="hint">{t("settings.aws.hint")}</p>
                        <div className="grid-2">
                            <div className="field">
                                <label htmlFor="aws-id">{t("settings.transcribe.accessKey")}</label>
                                <input id="aws-id" name="aws-id" type="text" autoComplete="off" spellCheck={false}
                                       placeholder={t("settings.secret.placeholder")}
                                       value={awsId} onChange={e => setAwsId(e.target.value)}/>
                            </div>
                            <div className="field">
                                <label htmlFor="aws-secret">{t("settings.transcribe.secretKey")}</label>
                                <input id="aws-secret" name="aws-secret" type="password" autoComplete="off" spellCheck={false}
                                       placeholder={t("settings.secret.placeholder")}
                                       value={awsSecret} onChange={e => setAwsSecret(e.target.value)}/>
                            </div>
                            <div className="field span-2">
                                <label htmlFor="aws-token">{t("settings.transcribe.sessionToken")}</label>
                                <input id="aws-token" name="aws-token" type="password" autoComplete="off" spellCheck={false}
                                       value={awsToken} onChange={e => setAwsToken(e.target.value)}/>
                            </div>
                        </div>
                        <div className="test-row">
                            <button type="button" className="btn btn-secondary"
                                    disabled={locked || awsId.trim() === '' || awsSecret.trim() === ''}
                                    onClick={() => secretAction('aws', () => SetAWSCredentials(awsId, awsSecret, awsToken), t("settings.saved"), () => {
                                        setAwsId('');
                                        setAwsSecret('');
                                        setAwsToken('');
                                    })}>
                                {t("settings.secret.save")}
                            </button>
                            <ConfirmDelete disabled={locked || !secrets.aws}
                                           onConfirm={() => secretAction('aws', () => DeleteAWSCredentials(), t("settings.secret.deleted"), () => undefined)}/>
                            <Message msg={msgs['aws']}/>
                        </div>
                    </Block>

                    <Block title={t("settings.section.gateway")}>
                        <div className="grid-2">
                            <div className="field">
                                <label htmlFor="gw-auth">{t("settings.gateway.auth")}</label>
                                <select id="gw-auth" name="gw-auth" value={form.gateway.auth}
                                        onChange={e => patch('gateway', {auth: e.target.value})}>
                                    <option value="sigv4">{t("settings.gateway.auth.sigv4")}</option>
                                    <option value="api_key">{t("settings.gateway.auth.api_key")}</option>
                                    <option value="bearer">{t("settings.gateway.auth.bearer")}</option>
                                </select>
                            </div>
                            <div className="field">
                                <label htmlFor="gw-region">{t("settings.gateway.region")}</label>
                                <input id="gw-region" name="gw-region" type="text" spellCheck={false} autoComplete="off"
                                       value={form.gateway.region}
                                       onChange={e => patch('gateway', {region: e.target.value})}/>
                            </div>
                            <div className="field span-2">
                                <label htmlFor="gw-model">{t("settings.gateway.model")}</label>
                                <input id="gw-model" name="gw-model" type="text" spellCheck={false} autoComplete="off"
                                       value={form.gateway.modelId}
                                       onChange={e => patch('gateway', {modelId: e.target.value})}/>
                            </div>
                            <div className="field span-2">
                                <label htmlFor="gw-url">{t("settings.gateway.baseUrl")}</label>
                                <input id="gw-url" name="gw-url" type="text" inputMode="url" spellCheck={false}
                                       autoComplete="off" aria-describedby="gw-url-hint"
                                       value={form.gateway.baseUrl}
                                       onChange={e => patch('gateway', {baseUrl: e.target.value})}/>
                                <p id="gw-url-hint" className="hint">{t("settings.gateway.baseUrl.hint")}</p>
                            </div>
                        </div>
                        {usesKey && (
                            <SecretInput
                                id="gw-key"
                                label={t("settings.gateway.key")}
                                hint={t("settings.gateway.key.hint")}
                                value={gwKey}
                                onChange={setGwKey}
                                locked={locked}
                                isSet={secrets.gatewayKey}
                                badge
                                msg={msgs['gw-key']}
                                onSave={() => secretAction('gw-key', () => SetGatewayKey(gwKey), t("settings.saved"), () => setGwKey(''))}
                                onDelete={() => secretAction('gw-key', () => DeleteGatewayKey(), t("settings.secret.deleted"), () => undefined)}
                            />
                        )}
                        <TestRow
                            label={busy === 'ai-prod' ? t("settings.testing") : t("settings.test.bedrock")}
                            disabled={locked || mode !== 'prod'}
                            note={mode !== 'prod' ? t("settings.test.onlyProd") : ''}
                            msg={msgs['ai-prod']}
                            onClick={() => onTest('ai-prod')}
                        />
                    </Block>

                    <Block title={t("settings.section.transcribe")}>
                        <div className="grid-2">
                            <div className="field">
                                <label htmlFor="tr-region">{t("settings.transcribe.region")}</label>
                                <input id="tr-region" name="tr-region" type="text" spellCheck={false} autoComplete="off"
                                       value={form.transcribe.region}
                                       onChange={e => patch('transcribe', {region: e.target.value})}/>
                            </div>
                            <label className="field field-check">
                                <input id="tr-multi" name="tr-multi" type="checkbox"
                                       checked={form.transcribe.multiLanguage}
                                       onChange={e => patch('transcribe', {multiLanguage: e.target.checked})}/>
                                <span>{t("settings.transcribe.multi")}</span>
                            </label>
                        </div>
                        <TestRow
                            label={busy === 'transcribe' ? t("settings.testing") : t("settings.transcribe.test")}
                            disabled={locked}
                            note=""
                            msg={msgs['transcribe']}
                            onClick={() => onTest('transcribe')}
                        />
                    </Block>
                </div>
            </details>

            {/* ---------- Avansat ---------- */}
            <details ref={sectionRefs.advanced} className="panel group"
                     open={open.advanced} onToggle={toggle('advanced')}>
                <summary className="group-head">
                    <span className="group-title">{t("settings.advanced.title")}</span>
                    <span className="group-desc">{t("settings.advanced.desc")}</span>
                </summary>
                <div className="group-body">
                    <Block title={t("settings.section.bot")}>
                        <div className="grid-2">
                            <div className="field">
                                <label htmlFor="bot-name">{t("settings.bot.name")}</label>
                                <input id="bot-name" name="bot-name" type="text" autoComplete="off"
                                       value={form.bot.displayName}
                                       onChange={e => patch('bot', {displayName: e.target.value})}/>
                            </div>
                            <div className="field">
                                <label htmlFor="bot-email">{t("settings.bot.email")}</label>
                                <input id="bot-email" name="bot-email" type="email" autoComplete="off" spellCheck={false}
                                       value={form.bot.email}
                                       onChange={e => patch('bot', {email: e.target.value})}/>
                            </div>
                            <div className="field span-2">
                                <label htmlFor="bot-browser">{t("settings.bot.browser")}</label>
                                <input id="bot-browser" name="bot-browser" type="text" autoComplete="off" spellCheck={false}
                                       value={form.bot.browserPath}
                                       onChange={e => patch('bot', {browserPath: e.target.value})}/>
                            </div>
                            <div className="field">
                                <label htmlFor="bot-lobby">{t("settings.bot.lobby")}</label>
                                <input id="bot-lobby" name="bot-lobby" type="number" inputMode="numeric" min={1} step={1}
                                       value={form.bot.lobbyTimeoutMin}
                                       onChange={e => patch('bot', {lobbyTimeoutMin: num(e.target.value)})}/>
                            </div>
                            <div className="field">
                                <label htmlFor="bot-silence">{t("settings.bot.silence")}</label>
                                <input id="bot-silence" name="bot-silence" type="number" inputMode="numeric" min={1} step={1}
                                       value={form.bot.silenceEndMin}
                                       onChange={e => patch('bot', {silenceEndMin: num(e.target.value)})}/>
                            </div>
                            <label className="field field-check span-2">
                                <input id="bot-headless" name="bot-headless" type="checkbox"
                                       checked={form.bot.headless}
                                       onChange={e => patch('bot', {headless: e.target.checked})}/>
                                <span>{t("settings.bot.headless")}</span>
                            </label>
                        </div>
                    </Block>

                    <Block title={t("settings.section.output")}>
                        <div className="grid-2">
                            <div className="field span-2">
                                <label htmlFor="out-dir">{t("settings.output.dir")}</label>
                                <div className="inline">
                                    <input id="out-dir" name="out-dir" type="text" autoComplete="off" spellCheck={false}
                                           value={form.output.dir}
                                           onChange={e => patch('output', {dir: e.target.value})}/>
                                    <button type="button" className="btn btn-secondary" disabled={locked} onClick={onChooseDir}>
                                        {t("settings.output.choose")}
                                    </button>
                                </div>
                                <Message msg={msgs['dir']}/>
                            </div>
                            <div className="field">
                                <label htmlFor="out-retention">{t("settings.output.retention")}</label>
                                <input id="out-retention" name="out-retention" type="number" inputMode="numeric" min={1} step={1}
                                       value={form.output.retentionDays}
                                       onChange={e => patch('output', {retentionDays: num(e.target.value)})}/>
                            </div>
                            <label className="field field-check">
                                <input id="out-audio" name="out-audio" type="checkbox"
                                       checked={form.output.keepAudio}
                                       onChange={e => patch('output', {keepAudio: e.target.checked})}/>
                                <span>{t("settings.output.keepAudio")}</span>
                            </label>
                        </div>
                    </Block>
                </div>
            </details>

            {version && <p className="version">{t("app.version", version)}</p>}

            <div className="savebar">
                <button type="button" className="btn btn-primary" disabled={locked || !dirty} onClick={onSave}>
                    {busy === 'save' ? t("settings.saving") : t("settings.save")}
                </button>
                {dirty && !msgs['save'] && <span className="unsaved">{t("settings.unsaved")}</span>}
                <Message msg={msgs['save']}/>
                <span className="savebar-hint">{t("settings.save.hint")}</span>
            </div>
        </div>
    );
}

function Block({title, badge, children}: { title: string; badge?: ReactNode; children: ReactNode }) {
    return (
        <section className="block">
            <div className="block-head">
                <h2 className="block-title">{title}</h2>
                {badge}
            </div>
            {children}
        </section>
    );
}

function SecretInput(p: {
    id: string;
    label: string;
    hint: string;
    value: string;
    onChange: (v: string) => void;
    locked: boolean;
    isSet: boolean;
    badge?: boolean;
    msg: Msg | undefined;
    onSave: () => void;
    onDelete: () => void;
}) {
    return (
        <div className="field">
            <div className="secret-head">
                <label htmlFor={p.id}>{p.label}</label>
                {p.badge && <SecretBadge set={p.isSet}/>}
            </div>
            <div className="inline">
                <input
                    id={p.id}
                    name={p.id}
                    type="password"
                    autoComplete="off"
                    spellCheck={false}
                    aria-describedby={`${p.id}-hint`}
                    placeholder={p.isSet ? t("settings.secret.placeholder") : t("settings.secret.placeholderNew")}
                    value={p.value}
                    onChange={e => p.onChange(e.target.value)}
                />
                <button type="button" className="btn btn-secondary" disabled={p.locked || p.value.trim() === ''}
                        onClick={p.onSave}>
                    {t("settings.secret.save")}
                </button>
                <ConfirmDelete disabled={p.locked || !p.isSet} onConfirm={p.onDelete}/>
            </div>
            <p id={`${p.id}-hint`} className="hint">{p.hint}</p>
            <Message msg={p.msg ?? null}/>
        </div>
    );
}

// Deleting a stored key needs a second click: it cannot be read back.
function ConfirmDelete({disabled, onConfirm}: { disabled: boolean; onConfirm: () => void }) {
    const [asking, setAsking] = useState(false);
    if (!asking) {
        return (
            <button type="button" className="btn btn-ghost" disabled={disabled} onClick={() => setAsking(true)}>
                {t("settings.secret.delete")}
            </button>
        );
    }
    return (
        <span className="confirm" role="group" aria-label={t("settings.secret.confirm")}>
            <button type="button" className="btn btn-danger" disabled={disabled}
                    onClick={() => { setAsking(false); onConfirm(); }}>
                {t("settings.secret.confirm")}
            </button>
            <button type="button" className="btn btn-ghost" autoFocus onClick={() => setAsking(false)}>
                {t("common.cancel")}
            </button>
        </span>
    );
}

function TestRow({label, disabled, note, msg, onClick}: {
    label: string;
    disabled: boolean;
    note: string;
    msg: Msg | undefined;
    onClick: () => void;
}) {
    return (
        <div className="test-row">
            <button type="button" className="btn btn-secondary" disabled={disabled} onClick={onClick}>{label}</button>
            {note && <span className="hint">{note}</span>}
            <Message msg={msg ?? null}/>
        </div>
    );
}

function LocalItem({ok, label, optional}: { ok: boolean; label: string; optional?: boolean }) {
    const cls = ok ? 'check check-ok' : optional ? 'check check-optional' : 'check check-missing';
    return (
        <li className={cls}>
            <span className="check-mark" aria-hidden="true">{ok ? '✓' : optional ? '-' : '!'}</span>
            <span className="check-body">
                <span className="check-label">{label}</span>
                <span className="check-detail">
                    {ok ? t("settings.local.found") : optional ? t("settings.local.optionalMissing") : t("settings.local.missing")}
                </span>
            </span>
        </li>
    );
}

function SecretBadge({set}: { set: boolean }) {
    return (
        <span className={set ? 'badge badge-ok' : 'badge badge-warn'}>
            {set ? t("settings.configured") : t("settings.notConfigured")}
        </span>
    );
}

function ReadyBadge({ok}: { ok: boolean }) {
    return (
        <span className={ok ? 'badge badge-ok' : 'badge badge-warn'}>
            {ok ? t("settings.ready") : t("settings.incomplete")}
        </span>
    );
}

function Message({msg}: { msg: Msg }) {
    if (!msg) {
        return null;
    }
    return (
        <span className={msg.ok ? 'msg msg-ok' : 'msg msg-err'} role={msg.ok ? 'status' : 'alert'}>
            {msg.text}
        </span>
    );
}
