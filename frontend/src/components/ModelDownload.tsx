import {useEffect, useState} from 'react';
import {DownloadModel} from '../../wailsjs/go/app/App';
import {EventsOn} from '../../wailsjs/runtime/runtime';
import {t} from '../lib/i18n';
import {errorText} from '../lib/errors';

interface Progress {
    done: number;
    total: number;
    running: boolean;
    finished: boolean;
    error: string;
}

const mb = (n: number) => Math.round(n / 1e6);

// ModelDownload fetches the speech model when the transcriber is installed
// but the model is not. App refreshes readiness when the download finishes.
export function ModelDownload() {
    const [p, setP] = useState<Progress | null>(null);
    const [err, setErr] = useState('');

    useEffect(() => EventsOn('model_download', (x: Progress) => setP(x)), []);

    const start = () => {
        setErr('');
        setP({done: 0, total: 0, running: true, finished: false, error: ''});
        DownloadModel().catch(e => setErr(errorText(e)));
    };

    const running = !!p?.running && !p.finished && !p.error;
    const pct = p && p.total > 0 ? Math.floor((p.done / p.total) * 100) : 0;
    return (
        <div className="model-dl">
            {running ? (
                <>
                    <div className="progress" role="progressbar" aria-valuemin={0} aria-valuemax={100} aria-valuenow={pct}
                         aria-label={t("model.download.progress")}>
                        <div className="progress-fill" style={{transform: `scaleX(${pct / 100})`}}/>
                    </div>
                    <p className="hint" role="status">{t("model.download.running", pct, mb(p!.done), mb(p!.total))}</p>
                </>
            ) : (
                <button type="button" className="btn btn-secondary btn-sm" onClick={start}>
                    {p?.error ? t("model.download.retry") : t("model.download.start")}
                </button>
            )}
            {(p?.error || err) && <p className="hint hint-error" role="alert">{p?.error || err}</p>}
            {!running && !p?.error && <p className="hint">{t("model.download.hint")}</p>}
        </div>
    );
}
