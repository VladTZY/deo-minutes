import React from 'react'
import {createRoot} from 'react-dom/client'
import './style.css'
import App from './App'
import {Translations} from '../wailsjs/go/app/App'
import {setTranslations} from './lib/i18n'

const root = createRoot(document.getElementById('root')!)

// Strings must be loaded before the first render: every label goes through t().
Translations()
    .then(setTranslations)
    .catch(() => setTranslations({}))
    .finally(() => {
        root.render(
            <React.StrictMode>
                <App/>
            </React.StrictMode>
        )
    })
