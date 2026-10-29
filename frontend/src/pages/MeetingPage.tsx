import {FormEvent, ReactNode, useEffect, useRef, useState} from 'react';
import {
    Defaults,
    Generate,
    ImportAudio,
    OpenPath,
    Start,
    Stop,
    SuggestMeetingNo,
    Transcribe,
    Transcript,
} from '../../wailsjs/go/app/App';
import {app} from '../../wailsjs/go/models';
import {t} from '../lib/i18n';
import {errorText} from '../lib/errors';
import {
    Flow,
    flowOf,
    formatDuration,
    isActive,
    isLive,
    Mode,
    parseLine,
    StepIndex,
    stepOfState,
    toneOf,
} from '../lib/status';
import {aiReady, Check, checksFor, localReady, Readiness, SettingsSection} from '../lib/readiness';
import {ModelDownload} from '../components/ModelDownload';

interface Props {
    status: app.Status;
    lines: string[];
    mode: Mode;
    ready: Readiness;
    onClearLines: () => void;
    onShowHistory: () => void;
    onOpenSettings: (s: SettingsSection, field?: string) => void;
}

type Pending = 'start' | 'stop' | 'import' | 'transcribe' | 'generate' | null;

export default function MeetingPage(props: Props) {
    const {status, lines, mode, ready, onClearLines, onShowHistory, onOpenSettings} = props;

    const [link, setLink] = useState('');
    const [password, setPassword] = useState('');
    const [project, setProject] = useState('');
    const [meetingNo, setMeetingNo] = useState('');
    const [preparedBy, setPreparedBy] = useState('');
    const [pending, setPending] = useState<Pending>(null);
    const [actionError, setActionError] = useState('');
    // The session the user closed with "Ședință nouă": its result stays in Istoric.
    const [dismissed, setDismissed] = useState<string | null>(null);

    const active = isActive(status.state);

    useEffect(() => {
        Defaults()
            .then(d => {
                setProject(d.project ?? '');
                setMeetingNo(d.meetingNo > 0 ? String(d.meetingNo) : '');
                setPreparedBy(d.preparedBy ?? '');
            })
            .catch(() => undefined);
    }, []);

    // Suggest the next meeting number for the typed project (debounced).
    const userEditedProject = useRef(false);
    useEffect(() => {
        if (!userEditedProject.current) {
            return;
        }
        let stale = false;
        const id = window.setTimeout(() => {
            SuggestMeetingNo(project)
                .then(n => {
                    if (!stale && n > 0) {
                        setMeetingNo(String(n));
                    }
                })
                .catch(() => undefined);
        }, 350);
        return () => {
            stale = true;
            window.clearTimeout(id);
        };
    }, [project]);

    // Remember which step was running, to show where a failure happened
    // (the backend does not refresh Steps during a PROD run). Idempotent, so
    // updating the ref during render is safe.
    const lastStep = useRef<StepIndex | -1>(-1);
    const step = stepOfState(status.state);
    if (step >= 0) {
        lastStep.current = step;
    }
    const failedAt: StepIndex | -1 = status.state === 'error' ? lastStep.current : -1;

    const prevState = useRef(status.state);
    useEffect(() => {
        if (status.state === 'done' && prevState.current !== 'done') {
            // The password was only needed to join; the next number moves on.
            setPassword('');
            SuggestMeetingNo(project)
                .then(n => n > 0 && setMeetingNo(String(n)))
                .catch(() => undefined);
        }
        prevState.current = status.state;
    }, [status.state, project]);

    // Any running session (a new one, or a step started from Istoric) brings
    // the flow back after "Ședință nouă".
    useEffect(() => {
        if (active && dismissed !== null) {
            setDismissed(null);
        }
    }, [active, dismissed]);

    const setup = status.state === 'idle' || (!active && dismissed !== null && dismissed === status.sessionId);
    const flow = flowOf(status, setup, mode, failedAt);

    // A finished earlier step the user went back to, to read the transcript
    // or redo a step. Any change of state shows the live step again.
    const [view, setView] = useState<StepIndex | null>(null);
    useEffect(() => setView(null), [status.state, status.sessionId]);
    const sessionMode = status.mode || mode;
    const canView: [boolean, boolean, boolean] = [
        !setup && !active && (status.steps?.recorded ?? false) && sessionMode === 'demo',
        !setup && !active && (status.steps?.transcribed ?? false),
        false,
    ];
    const viewing = view !== null && canView[view] ? view : null;

    async function act(kind: Exclude<Pending, null>, fn: () => Promise<void>) {
        if (pending) {
            return;
        }
        setActionError('');
        setPending(kind);
        try {
            await fn();
        } catch (e) {
            setActionError(errorText(e));
        } finally {
            setPending(null);
        }
    }

    function onStart(e: FormEvent) {
        e.preventDefault();
        if (active) {
            return;
        }
        void act('start', async () => {
            onClearLines();
            await Start(app.StartRequest.createFrom({
                link: link.trim(),
                password,
                project: project.trim(),
                meetingNo: Number.parseInt(meetingNo, 10) || 0,
                preparedBy: preparedBy.trim(),
            }));
            setDismissed(null);
        });
    }

    const onImport = () => act('import', async () => {
        const id = await ImportAudio(project.trim(), Number.parseInt(meetingNo, 10) || 0);
        if (id) {
            onClearLines();
            setDismissed(null);
        }
    });

    const onStop = () => act('stop', () => Stop());

    const onTranscribe = () => act('transcribe', async () => {
        onClearLines();
        await Transcribe(status.sessionId);
    });

    const onGenerate = () => act('generate', () => Generate(status.sessionId));

    function onNew() {
        setActionError('');
        setDismissed(status.sessionId || null);
        onClearLines();
    }

    const checks = checksFor(ready, mode);
    const blocked = checks !== null && checks.some(c => !c.ok);

    const onResume = () => setView(null);
    let body: ReactNode;
    if (viewing === 0) {
        body = (
            <RecordedView
                localOk={localReady(ready.local)}
                pending={pending}
                again
                onTranscribe={onTranscribe}
                onResume={onResume}
                onNew={onNew}
                onOpenSettings={onOpenSettings}
            />
        );
    } else if (viewing === 1) {
        body = (
            <TranscribedView
                sessionId={status.sessionId}
                mode={sessionMode === 'prod' ? 'prod' : 'demo'}
                aiOk={aiReady(ready, sessionMode === 'prod' ? 'prod' : 'demo')}
                pending={pending}
                again={status.steps?.generated ?? false}
                onGenerate={onGenerate}
                onBack={canView[0] ? () => setView(0) : undefined}
                onResume={onResume}
                onNew={onNew}
                onOpenSettings={onOpenSettings}
            />
        );
    } else if (setup) {
        body = (
            <SetupView
                mode={mode}
                checks={checks}
                blocked={blocked}
                pending={pending}
                onSubmit={onStart}
                onImport={onImport}
                onOpenSettings={onOpenSettings}
                fields={{link, password, project, meetingNo, preparedBy}}
                set={{
                    link: setLink,
                    password: setPassword,
                    project: (v: string) => {
                        userEditedProject.current = true;
                        setProject(v);
                    },
                    meetingNo: setMeetingNo,
                    preparedBy: setPreparedBy,
                }}
            />
        );
    } else if (isLive(status.state)) {
        body = <LiveView status={status} lines={lines} pending={pending} onStop={onStop}/>;
    } else if (status.state === 'recorded') {
        body = (
            <RecordedView
                localOk={localReady(ready.local)}
                pending={pending}
                onTranscribe={onTranscribe}
                onNew={onNew}
                onOpenSettings={onOpenSettings}
            />
        );
    } else if (status.state === 'transcribing') {
        body = <TranscribingView status={status} pending={pending} onStop={onStop}/>;
    } else if (status.state === 'transcribed') {
        body = (
            <TranscribedView
                sessionId={status.sessionId}
                mode={mode}
                aiOk={aiReady(ready, mode)}
                pending={pending}
                onGenerate={onGenerate}
                onBack={canView[0] ? () => setView(0) : undefined}
                onNew={onNew}
                onOpenSettings={onOpenSettings}
            />
        );
    } else if (status.state === 'generating' || status.state === 'saving') {
        body = <GeneratingView status={status}/>;
    } else if (status.state === 'done') {
        body = <DoneView result={status.result} onTranscript={canView[1] ? () => setView(1) : undefined} onNew={onNew}/>;
    } else {
        body = (
            <ErrorView
                status={status}
                flow={flow}
                mode={mode}
                localOk={localReady(ready.local)}
                pending={pending}
                onTranscribe={onTranscribe}
                onGenerate={onGenerate}
                onTranscript={canView[1] ? () => setView(1) : undefined}
                onNew={onNew}
                onShowHistory={onShowHistory}
            />
        );
    }

    const warnings = setup ? [] : status.warnings ?? [];

    return (
        <div className="page meeting">
            <Stepper
                flow={flow}
                prod={(setup ? mode : status.mode || mode) === 'prod'}
                canView={canView}
                viewing={viewing}
                onView={i => setView(i)}
            />
            {body}
            {actionError && <p className="msg msg-err action-error" role="alert">{actionError}</p>}
            {warnings.length > 0 && (
                <div className="warnings" role="status">
                    <h2 className="sub-title">{t("main.warnings")}</h2>
                    <ul>
                        {warnings.map(w => <li key={w}>{w}</li>)}
                    </ul>
                </div>
            )}
        </div>
    );
}

// ---------- Stepper ----------

function Stepper({flow, prod, canView, viewing, onView}: {
    flow: Flow;
    prod: boolean;
    canView: [boolean, boolean, boolean];
    viewing: StepIndex | null;
    onView: (i: StepIndex) => void;
}) {
    const titles = [t("step.record"), t("step.transcribe"), t("step.generate")];
    const word = (s: string) => {
        switch (s) {
            case 'done':
                return t("step.state.done");
            case 'current':
                return t("step.state.current");
            case 'next':
                return t("step.state.next");
            case 'auto':
                return t("step.state.auto");
            case 'error':
                return t("step.state.error");
            default:
                return prod ? t("step.state.auto") : t("step.state.waiting");
        }
    };
    const here = viewing ?? flow.findIndex(s => s === 'current' || s === 'next' || s === 'error');
    return (
        <ol className="stepper" aria-label={t("step.label")}>
            {flow.map((s, i) => {
                const inner = (
                    <>
                        <span className="step-mark" aria-hidden="true">{s === 'done' ? '✓' : s === 'error' ? '!' : i + 1}</span>
                        <span className="step-text">
                            <span className="step-title">{titles[i]}</span>
                            <span className="step-state">{i === viewing ? t("step.state.viewing") : word(s)}</span>
                        </span>
                    </>
                );
                const cls = `step step-${s}${i === viewing ? ' step-viewing' : ''}`;
                return (
                    <li key={i} className={cls} aria-current={i === here ? 'step' : undefined}>
                        {canView[i] && i !== viewing ? (
                            <button type="button" className="step-btn" title={t("step.view")}
                                    onClick={() => onView(i as StepIndex)}>
                                {inner}
                            </button>
                        ) : inner}
                    </li>
                );
            })}
        </ol>
    );
}

// ---------- Step 1: setup ----------

interface Fields {
    link: string;
    password: string;
    project: string;
    meetingNo: string;
    preparedBy: string;
}

interface SetupProps {
    mode: Mode;
    checks: Check[] | null;
    blocked: boolean;
    pending: Pending;
    fields: Fields;
    set: { [K in keyof Fields]: (v: string) => void };
    onSubmit: (e: FormEvent) => void;
    onImport: () => void;
    onOpenSettings: (s: SettingsSection, field?: string) => void;
}

function SetupView({mode, checks, blocked, pending, fields, set, onSubmit, onImport, onOpenSettings}: SetupProps) {
    return (
        <div className="setup">
            <section className="panel setup-form" aria-labelledby="form-title">
                <h1 id="form-title" className="panel-title">{t("main.form.title")}</h1>
                <form onSubmit={onSubmit} noValidate>
                    <div className="fields">
                        <div className="field">
                            <label htmlFor="link">{t("main.link")}</label>
                            <input
                                id="link"
                                name="link"
                                type="text"
                                inputMode="url"
                                autoComplete="off"
                                spellCheck={false}
                                placeholder={t("main.link.placeholder")}
                                value={fields.link}
                                onChange={e => set.link(e.target.value)}
                            />
                        </div>
                        <div className="field-row">
                            <div className="field">
                                <label htmlFor="password">{t("main.password.optional")}</label>
                                <input
                                    id="password"
                                    name="password"
                                    type="password"
                                    autoComplete="off"
                                    aria-describedby="password-hint"
                                    value={fields.password}
                                    onChange={e => set.password(e.target.value)}
                                />
                                <p id="password-hint" className="hint">{t("main.password.hint")}</p>
                            </div>
                            <div className="field">
                                <label htmlFor="project">{t("main.project")}</label>
                                <input
                                    id="project"
                                    name="project"
                                    type="text"
                                    autoComplete="off"
                                    value={fields.project}
                                    onChange={e => set.project(e.target.value)}
                                />
                            </div>
                        </div>
                        <div className="field-row">
                            <div className="field field-narrow">
                                <label htmlFor="meetingNo">{t("main.meetingNo")}</label>
                                <input
                                    id="meetingNo"
                                    name="meetingNo"
                                    type="number"
                                    inputMode="numeric"
                                    min={1}
                                    step={1}
                                    value={fields.meetingNo}
                                    onChange={e => set.meetingNo(e.target.value)}
                                />
                            </div>
                            <div className="field">
                                <label htmlFor="preparedBy">{t("main.preparedBy")}</label>
                                <input
                                    id="preparedBy"
                                    name="preparedBy"
                                    type="text"
                                    autoComplete="name"
                                    value={fields.preparedBy}
                                    onChange={e => set.preparedBy(e.target.value)}
                                />
                            </div>
                        </div>
                    </div>
                    <div className="primary-row">
                        <button
                            type="submit"
                            className="btn btn-primary btn-lg"
                            disabled={pending !== null || blocked}
                            aria-describedby="start-hint"
                        >
                            {pending === 'start' ? t("main.starting") : t("main.start")}
                        </button>
                        <p id="start-hint" className="hint">
                            {blocked ? t("main.start.blocked") : mode === 'demo' ? t("main.start.hint.demo") : t("main.start.hint.prod")}
                        </p>
                    </div>
                </form>
            </section>

            <aside className="setup-side">
                <section className="panel side-panel" aria-labelledby="checks-title">
                    <h2 id="checks-title" className="panel-title">{t("ready.title")}</h2>
                    {checks === null ? (
                        <p className="muted" role="status">{t("ready.loading")}</p>
                    ) : (
                        <ul className="checks">
                            {checks.map(c => (
                                <li key={c.id} className={c.ok ? 'check check-ok' : 'check check-missing'}>
                                    <span className="check-mark" aria-hidden="true">{c.ok ? '✓' : '!'}</span>
                                    <span className="check-body">
                                        <span className="check-label">{c.label}</span>
                                        <span className="check-detail">{c.detail}</span>
                                        {!c.ok && (c.canDownload ? (
                                            <ModelDownload/>
                                        ) : (
                                            <button type="button" className="link-btn" onClick={() => onOpenSettings(c.fix, c.field)}>
                                                {t("ready.fix")}
                                            </button>
                                        ))}
                                    </span>
                                </li>
                            ))}
                        </ul>
                    )}
                </section>

                <section className="panel side-panel" aria-labelledby="import-title">
                    <h2 id="import-title" className="panel-title">{t("import.heading")}</h2>
                    <p className="side-text">{t("import.desc")}</p>
                    <button type="button" className="btn btn-secondary" disabled={pending !== null} onClick={onImport}>
                        {pending === 'import' ? t("import.busy") : t("import.button")}
                    </button>
                </section>
            </aside>
        </div>
    );
}

// ---------- Step 1 running: in the meeting ----------

function LiveView({status, lines, pending, onStop}: {
    status: app.Status;
    lines: string[];
    pending: Pending;
    onStop: () => void;
}) {
    const prod = status.mode === 'prod';
    const tone = toneOf(status.state);
    let note = '';
    if (status.state === 'recording') {
        note = prod ? t("live.recording.prod") : t("live.recording.demo");
    } else if (status.state === 'finishing_transcript') {
        note = t("live.finishing");
    } else {
        note = t("live.joining");
    }
    return (
        <section className="panel stage" aria-labelledby="live-title">
            <h1 id="live-title" className="sr-only">{t("step.record")}</h1>
            <div className="stage-head">
                <div className={`state state-${tone}`} id="live-state" role="status">
                    {tone === 'recording' && <span className="rec-mark" aria-hidden="true"/>}
                    <span>{status.label}</span>
                </div>
                <dl className="metrics">
                    <div className="metric">
                        <dt>{t("main.elapsed")}</dt>
                        <dd className="metric-value metric-time">
                            <Elapsed startedAt={status.startedAt} ticking={status.state === 'recording'}/>
                        </dd>
                    </div>
                    <div className="metric">
                        <dt>{t("main.participants")}</dt>
                        <dd className="metric-value tabular">{status.participants || 0}</dd>
                    </div>
                </dl>
            </div>
            <p className="stage-note">{note}</p>
            {status.canStop && (
                <div className="primary-row">
                    <button type="button" className="btn btn-danger btn-lg" disabled={pending !== null} onClick={onStop}>
                        {pending === 'stop' ? t("main.stopping") : t("main.stop")}
                    </button>
                    <p className="hint">{t("main.stop.hint")}</p>
                </div>
            )}
            {(prod || lines.length > 0) && <TranscriptBox lines={lines} title={t("main.transcript")} follow/>}
        </section>
    );
}

function Elapsed({startedAt, ticking}: { startedAt: number; ticking: boolean }) {
    const [now, setNow] = useState(() => Date.now());
    useEffect(() => {
        if (!ticking) {
            return;
        }
        setNow(Date.now());
        const id = window.setInterval(() => setNow(Date.now()), 1000);
        return () => window.clearInterval(id);
    }, [ticking]);
    const value = startedAt > 0 ? formatDuration(now - startedAt) : formatDuration(0);
    return <span className="tabular">{value}</span>;
}

// ---------- Step 2 (DEMO) ----------

function RecordedView({localOk, pending, again, onTranscribe, onResume, onNew, onOpenSettings}: {
    localOk: boolean;
    pending: Pending;
    again?: boolean;
    onTranscribe: () => void;
    onResume?: () => void;
    onNew: () => void;
    onOpenSettings: (s: SettingsSection, field?: string) => void;
}) {
    return (
        <section className="panel stage" aria-labelledby="stage-title">
            <h1 id="stage-title" className="stage-title">{t("recorded.title")}</h1>
            <p className="stage-note">{again ? t("recorded.desc.again") : t("recorded.desc")}</p>
            {!localOk && (
                <Missing text={t("ready.local.missing")} onFix={() => onOpenSettings('demo')}/>
            )}
            <div className="primary-row">
                <button type="button" className="btn btn-primary btn-lg" disabled={pending !== null || !localOk}
                        onClick={onTranscribe}>
                    {pending === 'transcribe' ? t("main.starting") : again ? t("recorded.action.again") : t("recorded.action")}
                </button>
                {onResume && (
                    <button type="button" className="btn btn-secondary" onClick={onResume}>{t("step.resume")}</button>
                )}
                <button type="button" className="btn btn-ghost" onClick={onNew}>{t("main.new")}</button>
            </div>
        </section>
    );
}

function TranscribingView({status, pending, onStop}: { status: app.Status; pending: Pending; onStop: () => void }) {
    const pct = Math.round(Math.min(1, Math.max(0, status.progress || 0)) * 100);
    return (
        <section className="panel stage" aria-labelledby="stage-title">
            <h1 id="stage-title" className="stage-title">{t("transcribing.title")}</h1>
            <p className="stage-note">{t("transcribing.desc")}</p>
            <div className="progress-wrap">
                <div
                    className="progress"
                    role="progressbar"
                    aria-labelledby="stage-title"
                    aria-valuemin={0}
                    aria-valuemax={100}
                    aria-valuenow={pct}
                >
                    <span className="progress-fill" style={{transform: `scaleX(${pct / 100})`}}/>
                </div>
                <span className="progress-pct tabular" aria-hidden="true">{t("transcribing.pct", pct)}</span>
            </div>
            {status.canStop && (
                <div className="primary-row">
                    <button type="button" className="btn btn-secondary" disabled={pending !== null} onClick={onStop}>
                        {pending === 'stop' ? t("main.stopping") : t("transcribing.stop")}
                    </button>
                    <p className="hint">{t("transcribing.stop.hint")}</p>
                </div>
            )}
        </section>
    );
}

// ---------- Step 3 ----------

function TranscribedView({sessionId, mode, aiOk, pending, again, onGenerate, onBack, onResume, onNew, onOpenSettings}: {
    sessionId: string;
    mode: Mode;
    aiOk: boolean;
    pending: Pending;
    again?: boolean;
    onGenerate: () => void;
    onBack?: () => void;
    onResume?: () => void;
    onNew: () => void;
    onOpenSettings: (s: SettingsSection, field?: string) => void;
}) {
    const [preview, setPreview] = useState<string[] | null>(null);
    const [err, setErr] = useState('');

    useEffect(() => {
        let stale = false;
        setPreview(null);
        setErr('');
        if (!sessionId) {
            return;
        }
        Transcript(sessionId)
            .then(l => !stale && setPreview(l ?? []))
            .catch(e => {
                if (!stale) {
                    setPreview([]);
                    setErr(errorText(e));
                }
            });
        return () => {
            stale = true;
        };
    }, [sessionId]);

    return (
        <section className="panel stage" aria-labelledby="stage-title">
            <div className="stage-split">
                <div>
                    <h1 id="stage-title" className="stage-title">{t("transcribed.title")}</h1>
                    <p className="stage-note">
                        {again ? t("transcribed.desc.again")
                            : mode === 'demo' ? t("transcribed.desc.demo") : t("transcribed.desc.prod")}
                    </p>
                </div>
                <div className="primary-row primary-row-end">
                    {onBack && (
                        <button type="button" className="btn btn-ghost" onClick={onBack}>{t("step.back.record")}</button>
                    )}
                    <button type="button" className="btn btn-ghost" onClick={onNew}>{t("main.new")}</button>
                    {onResume && (
                        <button type="button" className="btn btn-secondary" onClick={onResume}>{t("step.resume")}</button>
                    )}
                    <button type="button" className="btn btn-primary btn-lg" disabled={pending !== null || !aiOk}
                            onClick={onGenerate}>
                        {pending === 'generate' ? t("main.starting") : again ? t("transcribed.action.again") : t("transcribed.action")}
                    </button>
                </div>
            </div>
            {!aiOk && (
                <Missing
                    text={mode === 'demo' ? t("ready.openrouter.missing") : t("ready.ai.missing")}
                    onFix={() => onOpenSettings(mode, mode === 'demo' ? 'or-key' : 'gw-model')}
                />
            )}
            {err && <p className="msg msg-err" role="alert">{err}</p>}
            {preview === null ? (
                <p className="muted" role="status">{t("transcribed.loading")}</p>
            ) : (
                <TranscriptBox
                    lines={preview}
                    title={t("transcribed.preview", preview.length)}
                    tall
                />
            )}
        </section>
    );
}

function GeneratingView({status}: { status: app.Status }) {
    const saving = status.state === 'saving';
    return (
        <section className="panel stage" aria-labelledby="stage-title">
            <div role="status">
                <h1 id="stage-title" className="stage-title">{status.label}</h1>
            </div>
            <p className="stage-note">{t("generating.desc")}</p>
            <ol className="substeps">
                <li className={saving ? 'substep substep-done' : 'substep substep-current'}>
                    <span className="substep-mark" aria-hidden="true">{saving ? '✓' : '1'}</span>
                    {t("generating.ai")}
                </li>
                <li className={saving ? 'substep substep-current' : 'substep'}>
                    <span className="substep-mark" aria-hidden="true">2</span>
                    {t("generating.excel")}
                </li>
            </ol>
        </section>
    );
}

function DoneView({result, onTranscript, onNew}: { result?: app.Result; onTranscript?: () => void; onNew: () => void }) {
    const [err, setErr] = useState('');
    const c = result?.counts;
    const counts = [
        {label: t("main.count.actions"), n: c?.actions ?? 0},
        {label: t("main.count.decisions"), n: c?.decisions ?? 0},
        {label: t("main.count.risks"), n: c?.risks ?? 0},
        {label: t("main.count.interdependencies"), n: c?.interdependencies ?? 0},
        {label: t("main.count.changes"), n: c?.changes ?? 0},
    ];

    async function open(path: string) {
        setErr('');
        try {
            await OpenPath(path);
        } catch (e) {
            setErr(errorText(e));
        }
    }

    return (
        <section className="panel stage stage-done" aria-labelledby="stage-title">
            <div className="stage-split">
                <h1 id="stage-title" className="stage-title">{t("main.done.title")}</h1>
                <div className="primary-row primary-row-end">
                    <button type="button" className="btn btn-ghost" onClick={onNew}>{t("main.new")}</button>
                    {onTranscript && (
                        <button type="button" className="btn btn-secondary" onClick={onTranscript}>
                            {t("step.back.transcript")}
                        </button>
                    )}
                    {result?.folder && (
                        <button type="button" className="btn btn-secondary" onClick={() => open(result.folder)}>
                            {t("main.openFolder")}
                        </button>
                    )}
                    {result?.excelPath && (
                        <button type="button" className="btn btn-primary btn-lg" onClick={() => open(result.excelPath)}>
                            {t("main.openExcel")}
                        </button>
                    )}
                </div>
            </div>
            <dl className="counts">
                {counts.map(x => (
                    <div key={x.label} className="count">
                        <dt>{x.label}</dt>
                        <dd className="tabular">{x.n}</dd>
                    </div>
                ))}
            </dl>
            {err && <p className="msg msg-err" role="alert">{err}</p>}
        </section>
    );
}

function ErrorView({status, flow, mode, localOk, pending, onTranscribe, onGenerate, onTranscript, onNew, onShowHistory}: {
    status: app.Status;
    flow: Flow;
    mode: Mode;
    localOk: boolean;
    pending: Pending;
    onTranscribe: () => void;
    onGenerate: () => void;
    onTranscript?: () => void;
    onNew: () => void;
    onShowHistory: () => void;
}) {
    const at = flow.indexOf('error');
    const steps = status.steps;
    // Retry the failed step when its input exists.
    const canGenerate = !!status.sessionId && at === 2;
    const canTranscribe = !!status.sessionId && at === 1 && (steps?.recorded ?? false) && localOk
        && (mode === 'demo' || status.mode === 'demo');
    const stepName = [t("step.record"), t("step.transcribe"), t("step.generate")][Math.max(0, at)];
    return (
        <section className="panel stage" aria-labelledby="stage-title">
            <div className="alert alert-err" role="alert">
                <h1 id="stage-title" className="alert-title">{t("error.title", stepName)}</h1>
                <p>{status.error || t("err.unexpected")}</p>
            </div>
            <div className="primary-row">
                {canGenerate && (
                    <button type="button" className="btn btn-primary btn-lg" disabled={pending !== null} onClick={onGenerate}>
                        {t("error.retryGenerate")}
                    </button>
                )}
                {canTranscribe && (
                    <button type="button" className="btn btn-primary btn-lg" disabled={pending !== null} onClick={onTranscribe}>
                        {t("error.retryTranscribe")}
                    </button>
                )}
                {onTranscript && (
                    <button type="button" className="btn btn-secondary" onClick={onTranscript}>
                        {t("step.back.transcript")}
                    </button>
                )}
                <button type="button" className={canGenerate || canTranscribe ? 'btn btn-secondary' : 'btn btn-primary btn-lg'}
                        onClick={onNew}>
                    {at === 0 ? t("error.retryStart") : t("main.new")}
                </button>
                <button type="button" className="btn btn-ghost" onClick={onShowHistory}>{t("main.goHistory")}</button>
            </div>
            <p className="hint">{t("error.hint")}</p>
        </section>
    );
}

// ---------- Shared bits ----------

function Missing({text, onFix}: { text: string; onFix: () => void }) {
    return (
        <div className="alert alert-warn" role="alert">
            <p>{text}</p>
            <button type="button" className="link-btn" onClick={onFix}>{t("ready.fix")}</button>
        </div>
    );
}

function TranscriptBox({lines, title, follow, tall}: { lines: string[]; title: string; follow?: boolean; tall?: boolean }) {
    const box = useRef<HTMLDivElement>(null);
    const pinned = useRef(true);

    // Follow new lines unless the user scrolled up to read.
    useEffect(() => {
        const el = box.current;
        if (follow && el && pinned.current) {
            el.scrollTop = el.scrollHeight;
        }
    }, [lines, follow]);

    function onScroll() {
        const el = box.current;
        if (el) {
            pinned.current = el.scrollHeight - el.scrollTop - el.clientHeight < 24;
        }
    }

    return (
        <div className="transcript">
            <h2 className="sub-title" id="transcript-title">{title}</h2>
            <div
                ref={box}
                className={tall ? 'transcript-box transcript-tall' : 'transcript-box'}
                role="log"
                aria-labelledby="transcript-title"
                tabIndex={0}
                onScroll={onScroll}
            >
                {lines.length === 0 ? (
                    <p className="transcript-empty">{t("main.transcript.empty")}</p>
                ) : (
                    lines.map((raw, i) => {
                        const l = parseLine(raw);
                        return (
                            <p key={i} className="line">
                                <span className="line-time tabular">{l.time}</span>
                                <span className="line-speaker">{l.speaker}</span>
                                <span className="line-text">{l.text}</span>
                            </p>
                        );
                    })
                )}
            </div>
        </div>
    );
}
