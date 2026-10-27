import {ReactNode, useEffect, useState} from 'react';
import {
    ChooseOutputDir,
    DeleteAWSCredentials,
    DeleteGatewayKey,
    GetSettings,
    SaveSettings,
    SetAWSCredentials,
    SetGatewayKey,
    TestGateway,
    TestTranscribe,
    Version,
} from '../../wailsjs/go/app/App';
import {app} from '../../wailsjs/go/models';
import {t} from '../lib/i18n';
import {errorText} from '../lib/errors';

type Msg = { ok: boolean; text: string } | null;

// Editable part of the settings; secrets status is tracked separately so a
// refresh after storing a key never overwrites unsaved edits.
type Form = Pick<app.Settings, 'gateway' | 'transcribe' | 'bot' | 'output'>;

function toForm(s: app.Settings): Form {
    return {
        gateway: {...s.gateway},
        transcribe: {...s.transcribe},
        bot: {...s.bot},
        output: {...s.output},
    };
}

export default function SettingsPage() {
    const [form, setForm] = useState<Form | null>(null);
    const [saved, setSaved] = useState('');
    const [secrets, setSecrets] = useState<app.SecretStatus>({gatewayKey: false, aws: false});
    const [loadError, setLoadError] = useState('');
    const [version, setVersion] = useState('');

    const [busy, setBusy] = useState<string | null>(null);
    const [saveMsg, setSaveMsg] = useState<Msg>(null);
    const [gwTestMsg, setGwTestMsg] = useState<Msg>(null);
    const [trTestMsg, setTrTestMsg] = useState<Msg>(null);
    const [keyMsg, setKeyMsg] = useState<Msg>(null);
    const [awsMsg, setAwsMsg] = useState<Msg>(null);
    const [dirMsg, setDirMsg] = useState<Msg>(null);

    // Write-only secret inputs: never filled from the backend, cleared after use.
    const [gwKey, setGwKey] = useState('');
    const [awsId, setAwsId] = useState('');
    const [awsSecret, setAwsSecret] = useState('');
    const [awsToken, setAwsToken] = useState('');

    useEffect(() => {
        GetSettings()
            .then(s => {
                const f = toForm(s);
                setForm(f);
                setSaved(JSON.stringify(f));
                setSecrets(s.secrets ?? {gatewayKey: false, aws: false});
            })
            .catch(e => setLoadError(errorText(e)));
        Version().then(setVersion).catch(() => undefined);
    }, []);

    if (loadError) {
        return <div className="page"><p className="msg msg-err" role="alert">{loadError}</p></div>;
    }
    if (!form) {
        return <div className="page"><p className="muted" role="status">{t("settings.loading")}</p></div>;
    }

    const dirty = JSON.stringify(form) !== saved;

    function patch<K extends keyof Form>(section: K, values: Partial<Form[K]>) {
        setSaveMsg(null);
        setForm(f => (f ? {...f, [section]: {...f[section], ...values}} : f));
    }

    async function refreshSecrets() {
        try {
            const s = await GetSettings();
            setSecrets(s.secrets);
        } catch {
            // status badge stays as it was
        }
    }

    // persist saves the current form; returns an error text or '' on success.
    async function persist(): Promise<string> {
        if (!form) {
            return '';
        }
        try {
            await SaveSettings(app.Settings.createFrom({...form, secrets}));
            setSaved(JSON.stringify(form));
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
        setSaveMsg(null);
        const err = await persist();
        setSaveMsg(err ? {ok: false, text: err} : {ok: true, text: t("settings.saved")});
    });

    // Tests read the saved configuration, so pending edits are saved first.
    const onTest = (which: 'gateway' | 'transcribe') => run(which, async () => {
        const set = which === 'gateway' ? setGwTestMsg : setTrTestMsg;
        set(null);
        if (dirty) {
            const err = await persist();
            if (err) {
                set({ok: false, text: err});
                return;
            }
        }
        try {
            await (which === 'gateway' ? TestGateway() : TestTranscribe());
            set({ok: true, text: t("settings.test.ok")});
        } catch (e) {
            set({ok: false, text: errorText(e)});
        }
    });

    const onSaveKey = () => run('key', async () => {
        setKeyMsg(null);
        try {
            await SetGatewayKey(gwKey);
            setKeyMsg({ok: true, text: t("settings.saved")});
        } catch (e) {
            setKeyMsg({ok: false, text: errorText(e)});
        } finally {
            setGwKey('');
            await refreshSecrets();
        }
    });

    const onDeleteKey = () => run('key', async () => {
        setKeyMsg(null);
        try {
            await DeleteGatewayKey();
            setKeyMsg({ok: true, text: t("settings.secret.deleted")});
        } catch (e) {
            setKeyMsg({ok: false, text: errorText(e)});
        } finally {
            await refreshSecrets();
        }
    });

    const onSaveAws = () => run('aws', async () => {
        setAwsMsg(null);
        try {
            await SetAWSCredentials(awsId, awsSecret, awsToken);
            setAwsMsg({ok: true, text: t("settings.saved")});
        } catch (e) {
            setAwsMsg({ok: false, text: errorText(e)});
        } finally {
            setAwsId('');
            setAwsSecret('');
            setAwsToken('');
            await refreshSecrets();
        }
    });

    const onDeleteAws = () => run('aws', async () => {
        setAwsMsg(null);
        try {
            await DeleteAWSCredentials();
            setAwsMsg({ok: true, text: t("settings.secret.deleted")});
        } catch (e) {
            setAwsMsg({ok: false, text: errorText(e)});
        } finally {
            await refreshSecrets();
        }
    });

    const onChooseDir = () => run('dir', async () => {
        setDirMsg(null);
        try {
            const dir = await ChooseOutputDir();
            if (dir) {
                patch('output', {dir});
            }
        } catch (e) {
            setDirMsg({ok: false, text: errorText(e)});
        }
    });

    const num = (v: string) => Number.parseInt(v, 10) || 0;
    const locked = busy !== null;

    return (
        <div className="page settings">
            <h1 className="page-title">{t("settings.title")}</h1>

            <Section id="sec-gateway" title={t("settings.section.gateway")}>
                <div className="grid-2">
                    <div className="field span-2">
                        <label htmlFor="gw-url">{t("settings.gateway.baseUrl")}</label>
                        <input
                            id="gw-url"
                            name="gw-url"
                            type="text"
                            inputMode="url"
                            spellCheck={false}
                            autoComplete="off"
                            aria-describedby="gw-url-hint"
                            value={form.gateway.baseUrl}
                            onChange={e => patch('gateway', {baseUrl: e.target.value})}
                        />
                        <p id="gw-url-hint" className="hint">{t("settings.gateway.baseUrl.hint")}</p>
                    </div>
                    <div className="field">
                        <label htmlFor="gw-region">{t("settings.gateway.region")}</label>
                        <input
                            id="gw-region"
                            name="gw-region"
                            type="text"
                            spellCheck={false}
                            autoComplete="off"
                            value={form.gateway.region}
                            onChange={e => patch('gateway', {region: e.target.value})}
                        />
                    </div>
                    <div className="field">
                        <label htmlFor="gw-auth">{t("settings.gateway.auth")}</label>
                        <select
                            id="gw-auth"
                            name="gw-auth"
                            value={form.gateway.auth}
                            onChange={e => patch('gateway', {auth: e.target.value})}
                        >
                            <option value="sigv4">{t("settings.gateway.auth.sigv4")}</option>
                            <option value="api_key">{t("settings.gateway.auth.api_key")}</option>
                            <option value="bearer">{t("settings.gateway.auth.bearer")}</option>
                        </select>
                    </div>
                    <div className="field span-2">
                        <label htmlFor="gw-model">{t("settings.gateway.model")}</label>
                        <input
                            id="gw-model"
                            name="gw-model"
                            type="text"
                            spellCheck={false}
                            autoComplete="off"
                            value={form.gateway.modelId}
                            onChange={e => patch('gateway', {modelId: e.target.value})}
                        />
                    </div>
                </div>

                <div className="secret">
                    <div className="secret-head">
                        <label htmlFor="gw-key" className="secret-label">{t("settings.gateway.key")}</label>
                        <SecretBadge set={secrets.gatewayKey}/>
                    </div>
                    <p id="gw-key-hint" className="hint">{t("settings.gateway.key.hint")}</p>
                    <div className="inline">
                        <input
                            id="gw-key"
                            name="gw-key"
                            type="password"
                            autoComplete="off"
                            spellCheck={false}
                            aria-describedby="gw-key-hint"
                            placeholder={t("settings.secret.placeholder")}
                            value={gwKey}
                            onChange={e => setGwKey(e.target.value)}
                        />
                        <button type="button" className="btn btn-secondary" disabled={locked || gwKey.trim() === ''}
                                onClick={onSaveKey}>
                            {t("settings.save")}
                        </button>
                        <button type="button" className="btn btn-ghost" disabled={locked || !secrets.gatewayKey}
                                onClick={onDeleteKey}>
                            {t("settings.secret.delete")}
                        </button>
                    </div>
                    <Message msg={keyMsg}/>
                </div>

                <div className="test-row">
                    <button type="button" className="btn btn-secondary" disabled={locked} onClick={() => onTest('gateway')}>
                        {busy === 'gateway' ? t("settings.testing") : t("settings.gateway.test")}
                    </button>
                    <Message msg={gwTestMsg}/>
                </div>
            </Section>

            <Section id="sec-transcribe" title={t("settings.section.transcribe")}>
                <div className="grid-2">
                    <div className="field">
                        <label htmlFor="tr-region">{t("settings.transcribe.region")}</label>
                        <input
                            id="tr-region"
                            name="tr-region"
                            type="text"
                            spellCheck={false}
                            autoComplete="off"
                            value={form.transcribe.region}
                            onChange={e => patch('transcribe', {region: e.target.value})}
                        />
                    </div>
                    <label className="field field-check">
                        <input
                            id="tr-multi"
                            name="tr-multi"
                            type="checkbox"
                            checked={form.transcribe.multiLanguage}
                            onChange={e => patch('transcribe', {multiLanguage: e.target.checked})}
                        />
                        <span>{t("settings.transcribe.multi")}</span>
                    </label>
                </div>
                <div className="test-row">
                    <button type="button" className="btn btn-secondary" disabled={locked} onClick={() => onTest('transcribe')}>
                        {busy === 'transcribe' ? t("settings.testing") : t("settings.transcribe.test")}
                    </button>
                    <Message msg={trTestMsg}/>
                </div>
            </Section>

            <Section id="sec-aws" title={t("settings.section.aws")} badge={<SecretBadge set={secrets.aws}/>}>
                <p className="hint section-hint">{t("settings.aws.hint")}</p>
                <div className="grid-2">
                    <div className="field">
                        <label htmlFor="aws-id">{t("settings.transcribe.accessKey")}</label>
                        <input
                            id="aws-id"
                            name="aws-id"
                            type="text"
                            autoComplete="off"
                            spellCheck={false}
                            placeholder={t("settings.secret.placeholder")}
                            value={awsId}
                            onChange={e => setAwsId(e.target.value)}
                        />
                    </div>
                    <div className="field">
                        <label htmlFor="aws-secret">{t("settings.transcribe.secretKey")}</label>
                        <input
                            id="aws-secret"
                            name="aws-secret"
                            type="password"
                            autoComplete="off"
                            spellCheck={false}
                            placeholder={t("settings.secret.placeholder")}
                            value={awsSecret}
                            onChange={e => setAwsSecret(e.target.value)}
                        />
                    </div>
                    <div className="field span-2">
                        <label htmlFor="aws-token">{t("settings.transcribe.sessionToken")}</label>
                        <input
                            id="aws-token"
                            name="aws-token"
                            type="password"
                            autoComplete="off"
                            spellCheck={false}
                            value={awsToken}
                            onChange={e => setAwsToken(e.target.value)}
                        />
                    </div>
                </div>
                <div className="test-row">
                    <button
                        type="button"
                        className="btn btn-secondary"
                        disabled={locked || awsId.trim() === '' || awsSecret.trim() === ''}
                        onClick={onSaveAws}
                    >
                        {t("settings.save")}
                    </button>
                    <button type="button" className="btn btn-ghost" disabled={locked || !secrets.aws} onClick={onDeleteAws}>
                        {t("settings.secret.delete")}
                    </button>
                    <Message msg={awsMsg}/>
                </div>
            </Section>

            <Section id="sec-bot" title={t("settings.section.bot")}>
                <div className="grid-2">
                    <div className="field">
                        <label htmlFor="bot-name">{t("settings.bot.name")}</label>
                        <input
                            id="bot-name"
                            name="bot-name"
                            type="text"
                            autoComplete="off"
                            value={form.bot.displayName}
                            onChange={e => patch('bot', {displayName: e.target.value})}
                        />
                    </div>
                    <div className="field">
                        <label htmlFor="bot-email">{t("settings.bot.email")}</label>
                        <input
                            id="bot-email"
                            name="bot-email"
                            type="email"
                            autoComplete="off"
                            spellCheck={false}
                            value={form.bot.email}
                            onChange={e => patch('bot', {email: e.target.value})}
                        />
                    </div>
                    <div className="field span-2">
                        <label htmlFor="bot-browser">{t("settings.bot.browser")}</label>
                        <input
                            id="bot-browser"
                            name="bot-browser"
                            type="text"
                            autoComplete="off"
                            spellCheck={false}
                            value={form.bot.browserPath}
                            onChange={e => patch('bot', {browserPath: e.target.value})}
                        />
                    </div>
                    <div className="field">
                        <label htmlFor="bot-lobby">{t("settings.bot.lobby")}</label>
                        <input
                            id="bot-lobby"
                            name="bot-lobby"
                            type="number"
                            inputMode="numeric"
                            min={1}
                            step={1}
                            value={form.bot.lobbyTimeoutMin}
                            onChange={e => patch('bot', {lobbyTimeoutMin: num(e.target.value)})}
                        />
                    </div>
                    <div className="field">
                        <label htmlFor="bot-silence">{t("settings.bot.silence")}</label>
                        <input
                            id="bot-silence"
                            name="bot-silence"
                            type="number"
                            inputMode="numeric"
                            min={1}
                            step={1}
                            value={form.bot.silenceEndMin}
                            onChange={e => patch('bot', {silenceEndMin: num(e.target.value)})}
                        />
                    </div>
                    <label className="field field-check span-2">
                        <input
                            id="bot-headless"
                            name="bot-headless"
                            type="checkbox"
                            checked={form.bot.headless}
                            onChange={e => patch('bot', {headless: e.target.checked})}
                        />
                        <span>{t("settings.bot.headless")}</span>
                    </label>
                </div>
            </Section>

            <Section id="sec-output" title={t("settings.section.output")}>
                <div className="grid-2">
                    <div className="field span-2">
                        <label htmlFor="out-dir">{t("settings.output.dir")}</label>
                        <div className="inline">
                            <input
                                id="out-dir"
                                name="out-dir"
                                type="text"
                                autoComplete="off"
                                spellCheck={false}
                                value={form.output.dir}
                                onChange={e => patch('output', {dir: e.target.value})}
                            />
                            <button type="button" className="btn btn-secondary" disabled={locked} onClick={onChooseDir}>
                                {t("settings.output.choose")}
                            </button>
                        </div>
                        <Message msg={dirMsg}/>
                    </div>
                    <div className="field">
                        <label htmlFor="out-retention">{t("settings.output.retention")}</label>
                        <input
                            id="out-retention"
                            name="out-retention"
                            type="number"
                            inputMode="numeric"
                            min={1}
                            step={1}
                            value={form.output.retentionDays}
                            onChange={e => patch('output', {retentionDays: num(e.target.value)})}
                        />
                    </div>
                    <label className="field field-check">
                        <input
                            id="out-audio"
                            name="out-audio"
                            type="checkbox"
                            checked={form.output.keepAudio}
                            onChange={e => patch('output', {keepAudio: e.target.checked})}
                        />
                        <span>{t("settings.output.keepAudio")}</span>
                    </label>
                </div>
            </Section>

            {version && <p className="version">{t("app.version", version)}</p>}

            <div className="savebar">
                <button type="button" className="btn btn-primary" disabled={locked} onClick={onSave}>
                    {busy === 'save' ? t("settings.saving") : t("settings.save")}
                </button>
                {dirty && !saveMsg && <span className="muted">{t("settings.unsaved")}</span>}
                <Message msg={saveMsg}/>
                <span className="savebar-hint">{t("settings.test.hint")}</span>
            </div>
        </div>
    );
}

function Section({id, title, badge, children}: { id: string; title: string; badge?: ReactNode; children: ReactNode }) {
    return (
        <section className="panel settings-section" aria-labelledby={id}>
            <div className="section-head">
                <h2 id={id} className="panel-title">{title}</h2>
                {badge}
            </div>
            {children}
        </section>
    );
}

function SecretBadge({set}: { set: boolean }) {
    return (
        <span className={set ? 'badge badge-ok' : 'badge'}>
            {set ? t("settings.configured") : t("settings.notConfigured")}
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
