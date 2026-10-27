import {useCallback, useEffect, useState} from 'react';
import {DeleteHistory, History, OpenPath, Resume} from '../../wailsjs/go/app/App';
import {app} from '../../wailsjs/go/models';
import {t} from '../lib/i18n';
import {errorText} from '../lib/errors';

interface Props {
    refreshKey: number;
    busy: boolean;
    onResumed: () => void;
}

export default function HistoryPage({refreshKey, busy, onResumed}: Props) {
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

    const onOpen = (it: app.HistoryItem) => act(it.id, () => OpenPath(it.excelPath));

    const onResume = (it: app.HistoryItem) => act(it.id, async () => {
        await Resume(it.id);
        onResumed();
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

            {error && <p className="msg msg-err" role="alert">{error}</p>}

            {items === null && <p className="muted" role="status">{t("history.loading")}</p>}

            {items !== null && items.length === 0 && !error && (
                <div className="empty">
                    <p>{t("history.empty")}</p>
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
                            <th scope="col" className="num">{t("history.no")}</th>
                            <th scope="col" className="num"><abbr title={t("main.count.actions")}>{t("history.short.actions")}</abbr></th>
                            <th scope="col" className="num"><abbr title={t("main.count.decisions")}>{t("history.short.decisions")}</abbr></th>
                            <th scope="col" className="num"><abbr title={t("main.count.risks")}>{t("history.short.risks")}</abbr></th>
                            <th scope="col" className="num"><abbr title={t("main.count.interdependencies")}>{t("history.short.interdependencies")}</abbr></th>
                            <th scope="col" className="num"><abbr title={t("main.count.changes")}>{t("history.short.changes")}</abbr></th>
                            <th scope="col"><span className="sr-only">{t("history.ops")}</span></th>
                        </tr>
                        </thead>
                        <tbody>
                        {items.map(it => {
                            const c = it.counts;
                            const rowBusy = pending === it.id;
                            return (
                                <tr key={it.id}>
                                    <td className="nowrap tabular">{it.date}</td>
                                    <td className="project-cell" title={it.project}>{it.project}</td>
                                    <td className="num tabular">{it.meetingNo}</td>
                                    <td className="num tabular">{c?.actions ?? 0}</td>
                                    <td className="num tabular">{c?.decisions ?? 0}</td>
                                    <td className="num tabular">{c?.risks ?? 0}</td>
                                    <td className="num tabular">{c?.interdependencies ?? 0}</td>
                                    <td className="num tabular">{c?.changes ?? 0}</td>
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
                                                {it.excelPath && (
                                                    <button type="button" className="btn btn-secondary btn-sm" disabled={rowBusy}
                                                            onClick={() => onOpen(it)}>
                                                        {t("main.openExcel")}
                                                    </button>
                                                )}
                                                {it.canResume && (
                                                    <button type="button" className="btn btn-primary btn-sm" disabled={rowBusy || busy}
                                                            onClick={() => onResume(it)}>
                                                        {t("history.resume")}
                                                    </button>
                                                )}
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
