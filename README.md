# DEO Minute

DEO Minute intră într-o ședință Webex ca participant, transcrie discuția și completează automat registrul de proiect (Excel) cu acțiunile, deciziile, riscurile, dependențele și modificările discutate. Fișierul se salvează în Documente.

Aplicația rulează local pe Windows, fără drepturi de administrator și fără instalare.

## Cerințe

- Windows 10 sau 11, cu Microsoft Edge (vine cu Windows).
- Conexiune la internet.
- Chei AWS pentru Amazon Transcribe, regiunea `eu-central-1` (Frankfurt).
- Acces la modelul AI: prin gateway-ul companiei sau direct prin AWS Bedrock.

Cheile le primești de la persoana care administrează contul AWS al companiei.

## Prima pornire

1. Dezarhivează `DEOMinute-<versiune>-windows-amd64.zip` într-un folder al tău, de exemplu `Documente\DEO Minute app`.
2. Pornește `DEOMinute.exe`. Dacă Windows afișează „Windows a protejat computerul”, apasă **Mai multe informații**, apoi **Rulează oricum**. Aplicația nu este semnată digital, de aceea apare mesajul.
3. Deschide **Setări** și completează:
   - **Chei AWS**: Access Key ID și Secret Access Key. Se salvează în Windows Credential Manager, nu într-un fișier.
   - **Gateway AI**: adresa gateway-ului, dacă există. Lasă câmpul gol pentru acces direct la AWS Bedrock.
   - **Autentificare**: „Chei AWS” dacă folosești aceleași chei ca la transcriere; altfel cheia gateway-ului sau cheia API Bedrock.
4. Apasă **Testează transcrierea** și **Testează gateway**. Ambele trebuie să afișeze „Conexiune reușită”.

## Înregistrarea unei ședințe

1. În pagina **Ședință**, lipește linkul Webex.
2. Completează parola ședinței, dacă are. Parola nu se salvează nicăieri.
3. Verifică proiectul, numărul întâlnirii și cine întocmește minuta. Aplicația le ține minte de la ședința anterioară.
4. Apasă **Pornește**. Botul intră în ședință cu numele „Asistent Minute (înregistrează)”.
5. Organizatorul trebuie să accepte botul din sala de așteptare.

Informează participanții că ședința este înregistrată și transcrisă.

Botul iese singur când ședința se termină, când este scos din ședință sau după 10 minute de liniște. Poți opri oricând cu **Oprește**; minuta se generează din ce s-a înregistrat până atunci.

După ședință, aplicația generează minuta (de obicei câteva minute pentru o ședință lungă) și afișează **Deschide Excel**.

## Unde se salvează fișierele

În `Documente\DEO Minute`:

- `AALLZZ_<Proiect>_Minuta_<Nr>.xlsx`: registrul completat, după șablonul companiei.
- `AALLZZ_<Proiect>_Minuta_<Nr>_transcriere.txt`: transcrierea, cu ora și numele vorbitorului.

Un fișier existent nu se suprascrie: a doua variantă primește sufixul `_v2`.

Fiecare rând din registru are în coloana Notes (sau într-un comentariu pe celula `#`, la Riscuri și Decizii) citatul din transcriere pe care se bazează, cu ora și vorbitorul. Rândurile pentru care citatul nu se găsește în transcriere sunt eliminate automat.

Fișierele de lucru (transcrierea brută, eventual audio) stau în `%LOCALAPPDATA%\DEOMinutes\sessions` și se șterg după 30 de zile (configurabil în Setări).

## Dacă generarea minutei eșuează

Transcrierea rămâne salvată. Corectează problema (de exemplu setările gateway-ului), apoi deschide **Istoric** și apasă **Reia procesarea** la ședința respectivă.

## Probleme frecvente

| Mesaj | Ce faci |
|-------|---------|
| Nu am găsit Microsoft Edge | Setează calea browserului în Setări → Bot. |
| Organizatorul nu a acceptat botul | Organizatorul trebuie să-l accepte din sala de așteptare Webex. |
| Ședința cere parolă | Completează câmpul „Parolă ședință”. |
| Cheile AWS pentru transcriere nu sunt configurate | Setări → Chei AWS. |
| Gateway-ul AI a refuzat cheia | Verifică cheia și tipul de autentificare din Setări → Gateway AI. |

Jurnalul aplicației, util pentru suport, este în `%LOCALAPPDATA%\DEOMinutes\logs`. Nu conține chei, parole sau textul transcrierii.

## Test pe o ședință reală (pentru suport)

Când Webex își schimbă pagina, botul poate să nu mai intre în ședință. Testul de mai jos arată pas cu pas ce vede botul:

```
set DEO_MEETING_PASSWORD=parola_sedintei
DEOMinute.exe --selftest --minutes 3 "https://firma.webex.com/meet/nume"
```

Adaugă `--visible` ca să vezi fereastra browserului. Rezultatele (capturi de ecran, `audio.wav`, `raport.txt`) apar în `%LOCALAPPDATA%\DEOMinutes\selftest`, iar folderul se deschide la final. Nu se trimite nimic în afara calculatorului.

## Pentru dezvoltatori

Planul, deciziile și întrebările deschise sunt în `PLAN.md`; convențiile pentru agenți în `CLAUDE.md`. Build: `scripts/build-windows.sh`.
