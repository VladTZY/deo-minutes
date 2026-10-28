# DEO Minute

DEO Minute intră într-o ședință Webex ca participant, transcrie discuția și completează automat registrul de proiect (Excel) cu acțiunile, deciziile, riscurile, dependențele și modificările discutate. Fișierul se salvează în Documente.

Aplicația rulează local pe Windows, fără drepturi de administrator și fără instalare.

## Cele două moduri

Comutatorul **DEMO | PROD** din colțul de sus alege cum se lucrează:

| | DEMO | PROD |
|---|---|---|
| Transcriere | pe laptop, după ședință (model pentru limba română) | Amazon Transcribe, în timpul ședinței |
| Completarea Excel-ului | OpenRouter | AWS Bedrock |
| Pași | 1. Înregistrează, 2. Transcrie, 3. Generează minuta: fiecare cu butonul lui | automat, după Pornește |

DEMO este potrivit pentru teste: poți relua orice pas și poți importa o înregistrare WAV fără să intri într-o ședință.

## Cerințe

- Windows 10 sau 11, cu Microsoft Edge (vine cu Windows).
- Conexiune la internet.
- Pentru DEMO: o cheie OpenRouter. Transcriptorul local vine în arhivă (folderul `whisper`) și are nevoie de Microsoft Visual C++ Redistributable 2015-2022 (x64), prezent pe majoritatea calculatoarelor. Dacă modelul de transcriere nu este în arhivă, aplicația îl descarcă o singură dată (574 MB) la apăsarea butonului **Descarcă modelul** din „Verificări înainte de ședință”.
- Pentru PROD: chei AWS pentru Amazon Transcribe (regiunea `eu-central-1`, Frankfurt) și acces la modelul AI prin gateway-ul companiei sau direct prin AWS Bedrock.

Cheile le primești de la persoana care administrează contul AWS al companiei.

## Prima pornire

1. Descarcă ultima versiune din [Releases](https://github.com/VladTZY/deo-minutes/releases/latest) (`DEOMinute-<versiune>-windows-amd64-lite.zip`) și dezarhiveaz-o într-un folder al tău, de exemplu `Documente\DEO Minute app`. Varianta `-lite` nu conține modelul de transcriere: aplicația îl descarcă la prima folosire (vezi Cerințe).
2. Pornește `DEOMinute.exe`. Dacă Windows afișează „Windows a protejat computerul”, apasă **Mai multe informații**, apoi **Rulează oricum**. Aplicația nu este semnată digital, de aceea apare mesajul.
3. Pagina **Ședință** arată în dreapta „Verificări înainte de ședință”: ce lipsește are un link direct spre setarea potrivită.
4. În **Setări**, pentru DEMO: cheia OpenRouter, apoi butonul de test. Pentru PROD: cheile AWS (Access Key ID și Secret Access Key), gateway-ul AI dacă există (gol înseamnă direct AWS Bedrock), apoi testele. Cheile se salvează în Windows Credential Manager, nu într-un fișier.

## Înregistrarea unei ședințe

1. În pagina **Ședință**, lipește linkul Webex.
2. Completează parola ședinței, dacă are. Parola nu se salvează nicăieri.
3. Verifică proiectul, numărul întâlnirii și cine întocmește minuta. Aplicația le ține minte de la ședința anterioară.
4. Apasă **Pornește**. Botul intră în ședință cu numele „Asistent Minute (înregistrează)”.
5. Organizatorul trebuie să accepte botul din sala de așteptare.

Informează participanții că ședința este înregistrată și transcrisă.

Botul iese singur când ședința se termină, când este scos din ședință, după 10 minute de liniște sau după 5 ore. Poți opri oricând cu **Oprește**; se păstrează ce s-a înregistrat până atunci.

În **PROD**, minuta se generează automat după ședință și apare **Deschide Excel**.

În **DEMO**, apeși pe rând:

1. **Transcrie**: rulează pe laptop. Durează aproximativ cât ședința sau mai mult, în funcție de procesor; bara arată progresul. Poți opri și relua.
2. Verifici textul transcrierii afișat pe ecran.
3. **Generează minuta**: completează Excel-ul prin OpenRouter, apoi apare **Deschide Excel**.

Pentru un test fără ședință, folosește **Importă o înregistrare WAV** (16 kHz, mono, 16 biți) și continuă de la pasul 2.

## Unde se salvează fișierele

În `Documente\DEO Minute`:

- `AALLZZ_<Proiect>_Minuta_<Nr>.xlsx`: registrul completat, după șablonul companiei.
- `AALLZZ_<Proiect>_Minuta_<Nr>_transcriere.txt`: transcrierea, cu ora și numele vorbitorului.

Un fișier existent nu se suprascrie: a doua variantă primește sufixul `_v2`.

Fiecare rând din registru are în coloana Notes (sau într-un comentariu pe celula `#`, la Riscuri și Decizii) citatul din transcriere pe care se bazează, cu ora și vorbitorul. Rândurile pentru care citatul nu se găsește în transcriere sunt eliminate automat.

Fișierele de lucru (transcrierea brută, eventual audio) stau în `%LOCALAPPDATA%\DEOMinutes\sessions` și se șterg după 30 de zile (configurabil în Setări).

## Dacă un pas eșuează

Înregistrarea și transcrierea rămân salvate. Corectează problema (de exemplu cheia OpenRouter sau setările gateway-ului), apoi deschide **Istoric**: fiecare ședință are butonul pentru pasul următor (**Transcrie**, **Generează** sau **Deschide Excel**).

## Probleme frecvente

| Mesaj | Ce faci |
|-------|---------|
| Nu am găsit Microsoft Edge | Setează calea browserului în Setări → Bot. |
| Organizatorul nu a acceptat botul | Organizatorul trebuie să-l accepte din sala de așteptare Webex. |
| Ședința cere parolă | Completează câmpul „Parolă ședință”. |
| Cheile AWS pentru transcriere nu sunt configurate | Setări → Chei AWS. |
| Gateway-ul AI a refuzat cheia | Verifică cheia și tipul de autentificare din Setări → Mod PROD. |
| Contul OpenRouter nu mai are credit | Adaugă credit în contul OpenRouter. |
| Transcriptorul local are nevoie de Microsoft Visual C++ | Cere echipei IT „Visual C++ Redistributable 2015-2022 x64” sau o arhivă care îl include. |

Jurnalul aplicației, util pentru suport, este în `%LOCALAPPDATA%\DEOMinutes\logs`. Nu conține chei, parole sau textul transcrierii.

## Test pe o ședință reală (pentru suport)

Când Webex își schimbă pagina, botul poate să nu mai intre în ședință. Testul de mai jos arată pas cu pas ce vede botul:

```
set DEO_MEETING_PASSWORD=parola_sedintei
DEOMinute.exe --selftest --minutes 3 "https://firma.webex.com/meet/nume"
```

Adaugă `--visible` ca să vezi fereastra browserului. Rezultatele (capturi de ecran, `audio.wav`, `raport.txt`) apar în `%LOCALAPPDATA%\DEOMinutes\selftest`, iar folderul se deschide la final. Nu se trimite nimic în afara calculatorului.

## Pentru dezvoltatori

Planul, deciziile și întrebările deschise sunt în `PLAN.md`; convențiile pentru agenți în `CLAUDE.md`. Transcriptorul local: `HF_TOKEN=… scripts/prepare-local-stt.sh` (sau `MODEL=stock` pentru test). Build: `scripts/build-windows.sh`.
