import {useCallback, useEffect, useState} from 'react';
import {Status} from '../wailsjs/go/app/App';
import {EventsOn} from '../wailsjs/runtime/runtime';
import {app} from '../wailsjs/go/models';
import {t} from './lib/i18n';
import {IDLE_STATUS, isActive} from './lib/status';
import MeetingPage from './pages/MeetingPage';
import SettingsPage from './pages/SettingsPage';
import HistoryPage from './pages/HistoryPage';

type Tab = 'main' | 'settings' | 'history';

const TAIL = 30;

function App() {
    const [tab, setTab] = useState<Tab>('main');
    const [status, setStatus] = useState<app.Status>(IDLE_STATUS);
    const [lines, setLines] = useState<string[]>([]);
    const [historyRev, setHistoryRev] = useState(0);

    useEffect(() => {
        Status().then(setStatus).catch(() => undefined);
        const offStatus = EventsOn('status', (s: app.Status) => {
            setStatus(s);
            if (s.state === 'done' || s.state === 'error') {
                setHistoryRev(r => r + 1);
            }
        });
        const offTranscript = EventsOn('transcript', (line: string) => {
            setLines(prev => {
                const next = prev.length >= TAIL ? prev.slice(prev.length - TAIL + 1) : prev.slice();
                next.push(line);
                return next;
            });
        });
        return () => {
            offStatus();
            offTranscript();
        };
    }, []);

    const clearTranscript = useCallback(() => setLines([]), []);
    const showMeeting = useCallback(() => {
        setLines([]);
        setTab('main');
    }, []);
    const showHistory = useCallback(() => setTab('history'), []);

    const active = isActive(status.state);
    const tabs: { id: Tab; label: string }[] = [
        {id: 'main', label: t("nav.main")},
        {id: 'settings', label: t("nav.settings")},
        {id: 'history', label: t("nav.history")},
    ];

    return (
        <div className="shell">
            <header className="topbar">
                <span className="brand" translate="no">{t("app.title")}</span>
                <nav className="tabs" aria-label={t("nav.label")}>
                    {tabs.map(x => (
                        <button
                            key={x.id}
                            type="button"
                            className="tab"
                            aria-current={tab === x.id ? 'page' : undefined}
                            onClick={() => setTab(x.id)}
                        >
                            {x.label}
                            {x.id === 'main' && active && tab !== 'main' && (
                                <span className="tab-note">{status.label}</span>
                            )}
                        </button>
                    ))}
                </nav>
            </header>
            <main className="content">
                {/* The meeting page stays mounted so a half-filled form survives tab switches. */}
                <div hidden={tab !== 'main'} className="page-host">
                    <MeetingPage
                        status={status}
                        lines={lines}
                        onStarted={clearTranscript}
                        onShowHistory={showHistory}
                    />
                </div>
                {/* Kept mounted too, so unsaved settings are not lost on a tab switch. */}
                <div hidden={tab !== 'settings'}>
                    <SettingsPage/>
                </div>
                {tab === 'history' && (
                    <HistoryPage refreshKey={historyRev} busy={active} onResumed={showMeeting}/>
                )}
            </main>
        </div>
    );
}

export default App
