import {useEffect, useState} from 'react';
import {Translations, Version} from "../wailsjs/go/app/App";

function App() {
    const [tr, setTr] = useState<Record<string, string>>({});
    const [version, setVersion] = useState('');
    useEffect(() => {
        Translations().then(setTr);
        Version().then(setVersion);
    }, []);
    return (
        <main style={{padding: 24}}>
            <h1>{tr["app.title"] ?? ""}</h1>
            <p>{version}</p>
        </main>
    );
}

export default App
