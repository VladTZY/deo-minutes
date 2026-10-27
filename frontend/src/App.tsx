import {useCallback, useEffect, useRef, useState} from 'react';
import {GetSettings, LocalStatus, Mode as GetMode, SetMode, Status} from '../wailsjs/go/app/App';
import {EventsOn} from '../wailsjs/runtime/runtime';
import {app} from '../wailsjs/go/models';
import {t} from './lib/i18n';
import {errorText} from './lib/errors';
import {IDLE_STATUS, isActive, Mode} from './lib/status';
import {Readiness, SettingsSection} from './lib/readiness';
import MeetingPage from './pages/MeetingPage';
import SettingsPage from './pages/SettingsPage';
import HistoryPage from './pages/HistoryPage';

type Tab = 'main' | 'history' | 'settings';

const TAIL = 40;

export interface SettingsFocus {
    section: SettingsSection;
    field?: string;
    n: number;
}

function App() {
    const [tab, setTab] = useState<Tab>('main');
    const [status, setStatus] = useState<app.Status>(IDLE_STATUS);
    const [lines, setLines] = useState<string[]>([]);
    const [historyRev, setHistoryRev] = useState(0);
    const [mode, setMode] = useState<Mode>('demo');
    const [modeError, setModeError] = useState('');
    const [modeBusy, setModeBusy] = useState(false);
    const [ready, setReady] = useState<Readiness>({settings: null, local: null});
    const [focus, setFocus] = useState<SettingsFocus | null>(null);
    const focusN = useRef(0);

    const refreshReady = useCallback(() => {
        GetSettings()
            .then(s => setReady(r => ({...r, settings: s})))
            .catch(() => undefined);
        LocalStatus()
            .then(l => setReady(r => ({...r, local: l})))
            .catch(() => undefined);
    }, []);

    useEffect(() => {
        Status().then(setStatus).catch(() => undefined);
        GetMode().then(m => setMode(m === 'prod' ? 'prod' : 'demo')).catch(() => undefined);
        refreshReady();
        const offStatus = EventsOn('status', (s: app.Status) => {
            setStatus(s);
            if (['recorded', 'transcribed', 'done', 'error', 'idle'].includes(s.state)) {
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
    }, [refreshReady]);

    const clearLines = useCallback(() => setLines([]), []);
    const showMain = useCallback(() => {
        setTab('main');
        refreshReady();
    }, [refreshReady]);
    const showHistory = useCallback(() => setTab('history'), []);
    const openSettings = useCallback((section: SettingsSection, field?: string) => {
        focusN.current += 1;
        setFocus({section, field, n: focusN.current});
        setTab('settings');
    }, []);

    const active = isActive(status.state);

    async function changeMode(m: Mode) {
        if (m === mode || active || modeBusy) {
            return;
        }
        setModeError('');
        setModeBusy(true);
        try {
            await SetMode(m);
            setMode(m);
            refreshReady();
        } catch (e) {
            setModeError(errorText(e));
        } finally {
            setModeBusy(false);
        }
    }

    const tabs: { id: Tab; label: string }[] = [
        {id: 'main', label: t("nav.main")},
        {id: 'history', label: t("nav.history")},
        {id: 'settings', label: t("nav.settings")},
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
                            onClick={() => (x.id === 'main' ? showMain() : setTab(x.id))}
                        >
                            {x.label}
                            {x.id === 'main' && active && tab !== 'main' && (
                                <span className="tab-note">{status.label}</span>
                            )}
                        </button>
                    ))}
                </nav>
                <fieldset className="mode-switch" disabled={active || modeBusy}>
                    <legend className="sr-only">{t("mode.label")}</legend>
                    {(['demo', 'prod'] as Mode[]).map(m => (
                        <label key={m} className="mode-opt" data-mode={m}>
                            <input
                                type="radio"
                                name="mode"
                                value={m}
                                checked={mode === m}
                                onChange={() => changeMode(m)}
                            />
                            <span>{m === 'demo' ? t("mode.demo") : t("mode.prod")}</span>
                        </label>
                    ))}
                </fieldset>
            </header>
            <div className={`mode-strip mode-strip-${mode}`} role="note">
                <span className="mode-strip-text">
                    <strong>{mode === 'demo' ? t("mode.demo") : t("mode.prod")}:</strong>{' '}
                    {mode === 'demo' ? t("mode.demo.desc") : t("mode.prod.desc")}
                </span>
                {modeError ? (
                    <span className="mode-strip-err" role="alert">{modeError}</span>
                ) : active ? (
                    <span className="mode-strip-lock">{t("mode.locked")}</span>
                ) : null}
            </div>
            <main className="content">
                {/* Kept mounted so a half-filled form survives tab switches. */}
                <div hidden={tab !== 'main'} className="page-host">
                    <MeetingPage
                        status={status}
                        lines={lines}
                        mode={mode}
                        ready={ready}
                        onClearLines={clearLines}
                        onShowHistory={showHistory}
                        onOpenSettings={openSettings}
                    />
                </div>
                <div hidden={tab !== 'settings'}>
                    <SettingsPage mode={mode} focus={focus} onChanged={refreshReady}/>
                </div>
                {tab === 'history' && (
                    <HistoryPage
                        refreshKey={historyRev}
                        busy={active}
                        onStepStarted={() => {
                            clearLines();
                            showMain();
                        }}
                    />
                )}
            </main>
        </div>
    );
}

export default App
