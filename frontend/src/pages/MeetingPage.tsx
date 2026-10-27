import {FormEvent, useEffect, useRef, useState} from 'react';
import {Defaults, OpenPath, Start, Stop, SuggestMeetingNo} from '../../wailsjs/go/app/App';
import {app} from '../../wailsjs/go/models';
import {t} from '../lib/i18n';
import {errorText} from '../lib/errors';
import {formatDuration, isActive, parseLine, phaseIndex, toneOf} from '../lib/status';

interface Props {
    status: app.Status;
    lines: string[];
    onStarted: () => void;
    onShowHistory: () => void;
}

export default function MeetingPage({status, lines, onStarted, onShowHistory}: Props) {
    const [link, setLink] = useState('');
    const [password, setPassword] = useState('');
    const [project, setProject] = useState('');
    const [meetingNo, setMeetingNo] = useState('');
    const [preparedBy, setPreparedBy] = useState('');
    const [pending, setPending] = useState<'start' | 'stop' | null>(null);
    const [formError, setFormError] = useState('');

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

    // Suggest the next meeting number for the typed project (debounced; stale
    // answers are dropped).
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

    // After a finished meeting the next number moves on; the password was only
    // needed to join and is dropped.
    const prevState = useRef(status.state);
    useEffect(() => {
        if (status.state === 'done' && prevState.current !== 'done') {
            setPassword('');
            SuggestMeetingNo(project)
                .then(n => n > 0 && setMeetingNo(String(n)))
                .catch(() => undefined);
        }
        prevState.current = status.state;
    }, [status.state, project]);

    async function onSubmit(e: FormEvent) {
        e.preventDefault();
        if (active || pending) {
            return;
        }
        setFormError('');
        setPending('start');
        const req = app.StartRequest.createFrom({
            link: link.trim(),
            password,
            project: project.trim(),
            meetingNo: Number.parseInt(meetingNo, 10) || 0,
            preparedBy: preparedBy.trim(),
        });
        try {
            onStarted();
            await Start(req);
        } catch (err) {
            setFormError(errorText(err));
        } finally {
            setPending(null);
        }
    }

    async function onStop() {
        setPending('stop');
        try {
            await Stop();
        } catch (err) {
            setFormError(errorText(err));
        } finally {
            setPending(null);
        }
    }

    return (
        <div className="meeting">
            <section className="panel meeting-form" aria-labelledby="form-title">
                <h1 id="form-title" className="panel-title">{t("main.form.title")}</h1>
                <form onSubmit={onSubmit} noValidate>
                    <fieldset disabled={active} className="fields">
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
                                value={link}
                                onChange={e => setLink(e.target.value)}
                            />
                        </div>
                        <div className="field">
                            <label htmlFor="password">{t("main.password")}</label>
                            <input
                                id="password"
                                name="password"
                                type="password"
                                autoComplete="off"
                                aria-describedby="password-hint"
                                value={password}
                                onChange={e => setPassword(e.target.value)}
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
                                value={project}
                                onChange={e => {
                                    userEditedProject.current = true;
                                    setProject(e.target.value);
                                }}
                            />
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
                                    value={meetingNo}
                                    onChange={e => setMeetingNo(e.target.value)}
                                />
                            </div>
                            <div className="field">
                                <label htmlFor="preparedBy">{t("main.preparedBy")}</label>
                                <input
                                    id="preparedBy"
                                    name="preparedBy"
                                    type="text"
                                    autoComplete="off"
                                    value={preparedBy}
                                    onChange={e => setPreparedBy(e.target.value)}
                                />
                            </div>
                        </div>
                    </fieldset>
                    <div className="form-actions">
                        <button type="submit" className="btn btn-primary" disabled={active || pending !== null}>
                            {pending === 'start' ? t("main.starting") : t("main.start")}
                        </button>
                        <button
                            type="button"
                            className="btn btn-danger"
                            disabled={!status.canStop || pending !== null}
                            onClick={onStop}
                        >
                            {t("main.stop")}
                        </button>
                    </div>
                    {formError && <p className="msg msg-err" role="alert">{formError}</p>}
                </form>
            </section>

            <LivePanel status={status} lines={lines} onShowHistory={onShowHistory}/>
        </div>
    );
}

function LivePanel({status, lines, onShowHistory}: { status: app.Status; lines: string[]; onShowHistory: () => void }) {
    const tone = toneOf(status.state);
    const label = status.label || t("state.idle");
    const warnings = status.warnings ?? [];
    const showPhases = status.state !== 'idle' && status.state !== 'error';

    return (
        <section className="panel live" aria-labelledby="live-state">
            <div className="live-head">
                <div className={`state state-${tone}`} id="live-state" role="status">
                    {tone === 'recording' && <span className="rec-mark" aria-hidden="true"/>}
                    <span>{label}</span>
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
                        <dd className="metric-value">{status.participants || 0}</dd>
                    </div>
                </dl>
            </div>

            {showPhases && <Phases state={status.state}/>}

            {status.state === 'idle' && <p className="live-note">{t("main.idle.hint")}</p>}
            {status.state === 'generating' && <p className="live-note">{t("main.generating.hint")}</p>}

            {status.state === 'error' && (
                <div className="alert alert-err" role="alert">
                    <p className="alert-title">{t("main.error.title")}</p>
                    <p>{status.error || t("err.unexpected")}</p>
                    <p className="alert-hint">{t("main.error.resumeHint")}</p>
                    <button type="button" className="btn btn-secondary" onClick={onShowHistory}>
                        {t("main.goHistory")}
                    </button>
                </div>
            )}

            {status.state === 'done' && status.result && <DoneBlock result={status.result}/>}

            {warnings.length > 0 && (
                <div className="warnings">
                    <h2 className="sub-title">{t("main.warnings")}</h2>
                    <ul>
                        {warnings.map(w => <li key={w}>{w}</li>)}
                    </ul>
                </div>
            )}

            <Transcript lines={lines}/>
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

function Phases({state}: { state: string }) {
    const current = phaseIndex(state);
    const labels = [
        t("state.connecting"),
        t("state.recording"),
        t("state.finishing_transcript"),
        t("state.generating"),
        t("state.saving"),
    ];
    return (
        <ol className="phases" aria-label={t("main.phases")}>
            {labels.map((l, i) => {
                const cls = i < current ? 'phase phase-done' : i === current ? 'phase phase-current' : 'phase';
                return (
                    <li key={l} className={cls} aria-current={i === current ? 'step' : undefined}>
                        {l}
                    </li>
                );
            })}
        </ol>
    );
}

function DoneBlock({result}: { result: app.Result }) {
    const [err, setErr] = useState('');
    const c = result.counts;
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
        <div className="done">
            <p className="done-title">{t("main.done.title")}</p>
            <dl className="counts">
                {counts.map(x => (
                    <div key={x.label} className="count">
                        <dt>{x.label}</dt>
                        <dd className="tabular">{x.n}</dd>
                    </div>
                ))}
            </dl>
            <div className="done-actions">
                {result.excelPath && (
                    <button type="button" className="btn btn-primary" onClick={() => open(result.excelPath)}>
                        {t("main.openExcel")}
                    </button>
                )}
                {result.folder && (
                    <button type="button" className="btn btn-secondary" onClick={() => open(result.folder)}>
                        {t("main.openFolder")}
                    </button>
                )}
            </div>
            {err && <p className="msg msg-err" role="alert">{err}</p>}
        </div>
    );
}

function Transcript({lines}: { lines: string[] }) {
    const box = useRef<HTMLDivElement>(null);
    const pinned = useRef(true);

    // Follow new lines unless the user scrolled up to read.
    useEffect(() => {
        const el = box.current;
        if (el && pinned.current) {
            el.scrollTop = el.scrollHeight;
        }
    }, [lines]);

    function onScroll() {
        const el = box.current;
        if (el) {
            pinned.current = el.scrollHeight - el.scrollTop - el.clientHeight < 24;
        }
    }

    return (
        <div className="transcript">
            <h2 className="sub-title" id="transcript-title">{t("main.transcript")}</h2>
            <div
                ref={box}
                className="transcript-box"
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
                                {l.time && <span className="line-time tabular">{l.time}</span>}
                                {l.speaker && <span className="line-speaker">{l.speaker}</span>}
                                <span className="line-text">{l.text}</span>
                            </p>
                        );
                    })
                )}
            </div>
        </div>
    );
}
