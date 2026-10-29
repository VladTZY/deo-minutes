# DEO Minute

DEO Minute intră într-o ședință Webex ca participant, transcrie discuția și completează automat registrul de proiect (Excel) cu acțiunile, deciziile, riscurile, dependențele și modificările discutate. Fișierul se salvează în Documente.

Aplicația rulează local pe Windows, fără drepturi de administrator și fără instalare.

## Cele două moduri

Comutatorul **DEMO | PROD** din colțul de sus alege cum se lucrează:

| | DEMO | PROD |
|---|---|---|
| Transcriere | pe laptop, după ședință (model pentru limba română) | Amazon Transcribe, în timpul ședinței |
| Completarea Excel-ului | OpenRouter sau OpenAI | AWS Bedrock |
| Pași | 1. Înregistrează, 2. Transcrie, 3. Generează minuta: fiecare cu butonul lui | automat, după Pornește |

DEMO este potrivit pentru teste: poți relua orice pas și poți importa o înregistrare WAV fără să intri într-o ședință.

## Cerințe

- Windows 10 sau 11, cu Google Chrome. Dacă nu e instalat, îl poți instala de pe google.com/chrome fără drepturi de administrator. Fără Chrome, botul folosește Microsoft Edge.
- Conexiune la internet.
- Pentru DEMO: o cheie OpenRouter sau o cheie OpenAI. Transcriptorul local vine în arhivă (folderul `whisper`) și are nevoie de Microsoft Visual C++ Redistributable 2015-2022 (x64), prezent pe majoritatea calculatoarelor. Dacă modelul de transcriere nu este în arhivă, aplicația îl descarcă o singură dată (574 MB) la apăsarea butonului **Descarcă modelul** din „Verificări înainte de ședință”.
- Pentru PROD: chei AWS pentru Amazon Transcribe (regiunea `eu-central-1`, Frankfurt) și acces la modelul AI prin gateway-ul companiei sau direct prin AWS Bedrock.

Cheile le primești de la persoana care administrează contul AWS al companiei.

## Prima pornire

1. Descarcă ultima versiune din [Releases](https://github.com/VladTZY/deo-minutes/releases/latest) (`DEOMinute-<versiune>-windows-amd64-lite.zip`) și dezarhiveaz-o într-un folder al tău, de exemplu `Documente\DEO Minute app`. Varianta `-lite` nu conține modelul de transcriere: aplicația îl descarcă la prima folosire (vezi Cerințe).
2. Pornește `DEOMinute.exe`. Dacă Windows afișează „Windows a protejat computerul”, apasă **Mai multe informații**, apoi **Rulează oricum**. Aplicația nu este semnată digital, de aceea apare mesajul.
3. Pagina **Ședință** arată în dreapta „Verificări înainte de ședință”: ce lipsește are un link direct spre setarea potrivită.
4. În **Setări**, pentru DEMO: alegi furnizorul (OpenRouter sau OpenAI), introduci cheia lui, apeși **Salvează**, apoi butonul de test. Modelul implicit este `anthropic/claude-opus-5.5` la OpenRouter și `gpt-6-astra` la OpenAI; îl poți schimba în același loc. Pentru PROD: cheile AWS (Access Key ID și Secret Access Key), gateway-ul AI dacă există (gol înseamnă direct AWS Bedrock), apoi testele. Cheile se salvează în Windows Credential Manager, nu într-un fișier.

## Înregistrarea unei ședințe

1. În pagina **Ședință**, lipește linkul Webex.
2. Completează parola ședinței, dacă are. Parola nu se salvează nicăieri.
3. Verifică proiectul, numărul întâlnirii și cine întocmește minuta. Aplicația le ține minte de la ședința anterioară.
4. Apasă **Pornește**. Botul intră în ședință cu numele „Asistent Minute (înregistrează)”.
5. Organizatorul trebuie să accepte botul din sala de așteptare.

Informează participanții că ședința este înregistrată și transcrisă.

Botul iese singur când ședința se termină, când este scos din ședință, după 10 minute de liniște sau după 5 ore. Poți opri oricând cu **Oprește**; se păstrează ce s-a înregistrat până atunci.

În **PROD**, după ședință se generează automat ce era bifat ultima dată la pasul 3 (implicit minuta și rezumatul), apoi apar **Deschide Excel** și **Deschide rezumatul**.

În **DEMO**, apeși pe rând:

1. **Transcrie**: rulează pe laptop. Durează aproximativ cât ședința sau mai mult, în funcție de procesor; bara arată progresul. Poți opri și relua.
2. Verifici textul transcrierii afișat pe ecran.
3. Bifezi ce vrei să generezi: **Minuta (Excel)**, **Rezumat (Word)** sau amândouă, apoi apeși butonul. Se folosește OpenRouter sau OpenAI, după furnizorul ales în Setări. La final apar **Deschide Excel** și/sau **Deschide rezumatul**. Aplicația ține minte bifele pentru data viitoare.

Te poți întoarce oricând la un pas terminat: apasă pe pas în bara de sus sau pe **Vezi transcrierea**. De acolo poți citi transcrierea, poți genera minuta din nou (se salvează un fișier nou, `_v2`) sau poți transcrie din nou înregistrarea. **Înapoi la pasul curent** te readuce la rezultat.

Pentru un test fără ședință, folosește **Importă o înregistrare WAV** (16 kHz, mono, 16 biți) și continuă de la pasul 2.

## Unde se salvează fișierele

În `Documente\DEO Minute`:

- `AALLZZ_<Proiect>_Minuta_<Nr>.xlsx`: registrul completat, după șablonul companiei.
- `AALLZZ_<Proiect>_Minuta_<Nr>_rezumat.docx`: rezumatul ședinței (Word): pe scurt, subiecte discutate, decizii, acțiuni, puncte deschise, pași următori. Se generează doar dacă e bifat.
- `AALLZZ_<Proiect>_Minuta_<Nr>_transcriere.txt`: transcrierea, cu ora și numele vorbitorului.

Un fișier existent nu se suprascrie: a doua variantă primește sufixul `_v2`.

Fiecare rând din registru are în coloana Notes (sau într-un comentariu pe celula `#`, la Riscuri și Decizii) citatul din transcriere pe care se bazează, cu ora și vorbitorul. Rândurile pentru care citatul nu se găsește în transcriere sunt eliminate automat.

Restul datelor stau lângă `DEOMinute.exe`, în folderul aplicației:

```
DEO Minute app\
  DEOMinute.exe
  whisper\          transcriptorul local
  models\           modelul de transcriere
  data\
    config.json     setările (fără chei: cheile sunt în Windows Credential Manager)
    sessions\       fișierele de lucru ale fiecărei ședințe (audio, transcrierea brută)
    logs\           jurnalul aplicației
```

Fișierele din `data\sessions` se șterg după 30 de zile (configurabil în Setări). Ca să muți aplicația pe alt calculator, copiezi tot folderul; cheile le introduci din nou în Setări.

Dacă folderul aplicației nu permite scrierea (de exemplu e într-o locație protejată), aplicația folosește `%LOCALAPPDATA%\DEOMinutes`, ca versiunile mai vechi. La prima pornire a versiunii noi, datele din `%LOCALAPPDATA%\DEOMinutes` se mută automat în `data`.

## Dacă un pas eșuează

Înregistrarea și transcrierea rămân salvate. Corectează problema (de exemplu cheia OpenRouter/OpenAI sau setările gateway-ului), apoi deschide **Istoric**: fiecare ședință are butonul pentru pasul următor (**Transcrie**, **Generează** sau **Deschide Excel**).

Un clic pe o ședință din **Istoric** o deschide în pagina **Ședință**, la ultimul pas terminat. De acolo deschizi Excel-ul sau rezumatul, citești transcrierea, schimbi bifele **Minuta** și **Rezumat** și generezi din nou. Fișierele noi primesc `_v2`, `_v3`, cele vechi rămân. Cât timp rulează o ședință, cele din Istoric nu se pot deschide.

## Probleme frecvente

| Mesaj | Ce faci |
|-------|---------|
| Nu am găsit Google Chrome sau Microsoft Edge | Instalează Google Chrome sau setează calea browserului în Setări → Bot. |
| Aplicația rulează ca administrator | Închide aplicația și pornește `DEOMinute.exe` cu dublu-click, fără „Rulează ca administrator”. |
| Organizatorul nu a acceptat botul | Organizatorul trebuie să-l accepte din sala de așteptare Webex. |
| Ședința cere parolă | Completează câmpul „Parolă ședință”. |
| Cheile AWS pentru transcriere nu sunt configurate | Setări → Chei AWS. |
| Gateway-ul AI a refuzat cheia | Verifică cheia și tipul de autentificare din Setări → Mod PROD. |
| Contul OpenRouter nu mai are credit | Adaugă credit în contul OpenRouter. |
| Contul OpenAI nu mai are credit (insufficient_quota) | Adaugă credit în contul OpenAI (platform.openai.com → Billing). |
| Modelul OpenAI din Setări nu există | Verifică numele modelului în Setări → Mod DEMO, sau lasă câmpul gol pentru modelul implicit. |
| Transcriptorul local are nevoie de Microsoft Visual C++ | Cere echipei IT „Visual C++ Redistributable 2015-2022 x64” sau o arhivă care îl include. |

Jurnalul aplicației, util pentru suport, este în `data\logs`, lângă `DEOMinute.exe`. Nu conține chei, parole sau textul transcrierii.

## Test pe o ședință reală (pentru suport)

Când Webex își schimbă pagina, botul poate să nu mai intre în ședință. Testul de mai jos arată pas cu pas ce vede botul:

```
set DEO_MEETING_PASSWORD=parola_sedintei
DEOMinute.exe --selftest --minutes 3 "https://firma.webex.com/meet/nume"
```

Adaugă `--visible` ca să vezi fereastra browserului. Rezultatele (capturi de ecran, `audio.wav`, `raport.txt`) apar în `data\selftest`, lângă `DEOMinute.exe`, iar folderul se deschide la final. Nu se trimite nimic în afara calculatorului.

## Pentru dezvoltatori

Planul, deciziile și întrebările deschise sunt în `PLAN.md`; convențiile pentru agenți în `CLAUDE.md`. Transcriptorul local: `HF_TOKEN=… scripts/prepare-local-stt.sh` (sau `MODEL=stock` pentru test). Build: `scripts/build-windows.sh`.
