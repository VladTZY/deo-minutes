// Romanian strings come from internal/i18n/ro.json through the Translations
// binding. Always call t() with a literal key: the Go test
// TestAllReferencedKeysExist fails when such a key is missing from ro.json.

let table: Record<string, string> = {};

export function setTranslations(m: Record<string, string> | null | undefined): void {
    table = m ?? {};
}

// t returns the string for key with {0}, {1}… replaced by args. A missing key
// returns the key itself so the gap is visible instead of blank.
export function t(key: string, ...args: (string | number)[]): string {
    let s = table[key] ?? key;
    args.forEach((a, i) => {
        s = s.split(`{${i}}`).join(String(a));
    });
    return s;
}
