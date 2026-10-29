import {useCallback, useEffect, useState} from 'react';
import {DeleteHistory, Generate, History, OpenPath, OpenSession, Outputs, Transcribe} from '../../wailsjs/go/app/App';
import {app} from '../../wailsjs/go/models';
import {t} from '../lib/i18n';
import {errorText} from '../lib/errors';

interface Props {
    refreshKey: number;
    busy: boolean;
    onStepStarted: () => void;
    // A row was opened on the main page.
    onSessionOpened: () => void;
}

export default function HistoryPage({refreshKey, busy, onStepStarted, onSessionOpened}: Props) {
    const [items, setItems] = useState<app.HistoryItem[] | null>(null);
    const [error, setError] = useState('');
    const [confirmId, setConfirmId] = useState<string | null>(null);
    const [pending, setPending] = useState<string | null>(null);

    const load = useCallback(() => {
        History()
            .then(list => setItems(list ?? []))
            .catch(e => {
                setItems([]);
                setError(errorText(e));
            });
    }, []);

    useEffect(load, [load, refreshKey]);

    async function act(id: string, fn: () => Promise<void>) {
        setError('');
        setPending(id);
        try {
            await fn();
        } catch (e) {
            setError(errorText(e));
        } finally {
            setPending(null);
        }
    }

    const onOpen = (path: string) => act(path, () => OpenPath(path));
    const onTranscribe = (it: app.HistoryItem) => act(it.id, async () => {
        await Transcribe(it.id);
        onStepStarted();
    });
    const onGenerate = (it: app.HistoryItem) => act(it.id, async () => {
        await Generate(it.id, await Outputs()); // the last choice made in step 3
        onStepStarted();
    });
    const onOpenSession = (it: app.HistoryItem) => act(it.id, async () => {
        await OpenSession(it.id);
        onSessionOpened();
    });
    const onDelete = (it: app.HistoryItem) => act(it.id, async () => {
        await DeleteHistory(it.id);
        setConfirmId(null);
        load();
    });

    return (
        <div className="page history">
            <div className="page-head">
                <h1 className="page-title">{t("history.title")}</h1>
                <button type="button" className="btn btn-ghost" onClick={() => { setError(''); load(); }}>
                    {t("history.refresh")}
                </button>
            </div>

            {error && <p className="msg msg-err history-error" role="alert">{error}</p>}

            {items === null && <p className="muted" role="status">{t("history.loading")}</p>}

            {items !== null && items.length === 0 && !error && (
                <div className="empty">
                    <p className="empty-title">{t("history.empty")}</p>
                    <p className="muted">{t("history.empty.hint")}</p>
                </div>
            )}

            {items !== null && items.length > 0 && (
                <div className="table-wrap">
                    <table className="table">
                        <thead>
                        <tr>
                            <th scope="col">{t("history.date")}</th>
                            <th scope="col">{t("history.project")}</th>
                            <th scope="col">{t("history.mode")}</th>
                            <th scope="col">{t("history.steps")}</th>
                            <th scope="col"><span className="sr-only">{t("history.ops")}</span></th>
                        </tr>
                        </thead>
                        <tbody>
                        {items.map(it => {
                            const rowBusy = pending === it.id;
                            const c = it.counts;
                            const name = t("history.projectNo", it.project, it.meetingNo);
                            // The whole row opens the session; the name is the keyboard target.
                            return (
                                <tr key={it.id} className={busy ? undefined : 'row-link'}
                                    onClick={e => {
                                        if (!busy && !rowBusy && !(e.target as HTMLElement).closest('button')) {
                                            onOpenSession(it);
                                        }
                                    }}>
                                    <td className="nowrap tabular">{it.date}</td>
                                    <td className="project-cell">
                                        <button type="button" className="project-name row-open" disabled={busy || rowBusy}
                                                title={busy ? t("history.openRow.busy") : it.project}
                                                aria-label={t("history.openRow", name)}
                                                onClick={() => onOpenSession(it)}>
                                            {name}
                                        </button>
                                        {it.imported && (
                                            <span className="row-sub" title={it.imported}>{t("history.imported", it.imported)}</span>
                                        )}
                                        {it.excelPath && c && (
                                            <span className="row-sub">
                                                {t("history.counts", c.actions ?? 0, c.decisions ?? 0, c.risks ?? 0)}
                                            </span>
                                        )}
                                    </td>
                                    <td>
                                        <span className={`mode-tag mode-tag-${it.mode === 'prod' ? 'prod' : 'demo'}`}>
                                            {it.mode === 'prod' ? t("mode.prod") : t("mode.demo")}
                                        </span>
                                    </td>
                                    <td><StepMarks steps={it.steps}/></td>
                                    <td className="ops">
                                        {confirmId === it.id ? (
                                            <div className="confirm" role="group" aria-label={t("history.confirmDelete")}>
                                                <span>{t("history.confirmDelete")}</span>
                                                <button type="button" className="btn btn-danger btn-sm" disabled={rowBusy}
                                                        onClick={() => onDelete(it)}>
                                                    {t("history.delete")}
                                                </button>
                                                <button type="button" className="btn btn-ghost btn-sm" autoFocus
                                                        onClick={() => setConfirmId(null)}>
                                                    {t("common.cancel")}
                                                </button>
                                            </div>
                                        ) : (
                                            <div className="ops-buttons">
                                                <NextAction
                                                    it={it}
                                                    disabled={rowBusy || busy}
                                                    onOpen={onOpen}
                                                    onTranscribe={onTranscribe}
                                                    onGenerate={onGenerate}
                                                />
                                                <button type="button" className="btn btn-ghost btn-sm" disabled={rowBusy}
                                                        onClick={() => setConfirmId(it.id)}>
                                                    {t("history.delete")}
                                                </button>
                                            </div>
                                        )}
                                    </td>
                                </tr>
                            );
                        })}
                        </tbody>
                    </table>
                </div>
            )}
        </div>
    );
}

// The one next thing a row can do: open the result, or run the missing step.
function NextAction({it, disabled, onOpen, onTranscribe, onGenerate}: {
    it: app.HistoryItem;
    disabled: boolean;
    onOpen: (path: string) => void;
    onTranscribe: (it: app.HistoryItem) => void;
    onGenerate: (it: app.HistoryItem) => void;
}) {
    if (it.excelPath || it.summaryPath) {
        // Opening a file never conflicts with a running session.
        return (
            <>
                {it.excelPath && (
                    <button type="button" className="btn btn-primary btn-sm" onClick={() => onOpen(it.excelPath)}>
                        {t("main.openExcel")}
                    </button>
                )}
                {it.summaryPath && (
                    <button type="button" className={it.excelPath ? 'btn btn-secondary btn-sm' : 'btn btn-primary btn-sm'}
                            onClick={() => onOpen(it.summaryPath)}>
                        {t("history.openSummary")}
                    </button>
                )}
            </>
        );
    }
    if (it.canGenerate) {
        return (
            <button type="button" className="btn btn-primary btn-sm" disabled={disabled} onClick={() => onGenerate(it)}>
                {t("history.generate")}
            </button>
        );
    }
    if (it.canTranscribe) {
        return (
            <button type="button" className="btn btn-primary btn-sm" disabled={disabled} onClick={() => onTranscribe(it)}>
                {t("history.transcribe")}
            </button>
        );
    }
    return <span className="muted ops-none">{t("history.nothing")}</span>;
}

function StepMarks({steps}: { steps?: app.Steps }) {
    const s = [steps?.recorded, steps?.transcribed, steps?.generated];
    const names = [t("step.record"), t("step.transcribe"), t("step.generate")];
    return (
        <ol className="marks" aria-label={t("history.steps")}>
            {s.map((ok, i) => (
                <li key={i} className={ok ? 'mark mark-ok' : 'mark'} title={names[i]}>
                    <span aria-hidden="true">{ok ? '✓' : i + 1}</span>
                    <span className="sr-only">{names[i]}: {ok ? t("step.state.done") : t("history.stepMissing")}</span>
                </li>
            ))}
        </ol>
    );
}
