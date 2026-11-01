# Lyrics viewer

Lokalny widok karaoke w Astro + React + TypeScript. Duże wersy, płynne wypełnianie
słów, automatyczne śledzenie tekstu i sterowanie odtwarzaniem.

## Uruchomienie

Wymagane: Node.js **22.12+** oraz npm.

```bash
cd frontend
npm ci
npm run dev
```

Otwórz adres wypisany przez Astro, domyślnie **http://localhost:4321**.

```bash
npm run check
npm test
npm run build
npm run preview
```

W środowisku z ograniczonym dostępem do katalogu domowego poprzedź polecenia Astro
zmienną `ASTRO_TELEMETRY_DISABLED=1`, np. `ASTRO_TELEMETRY_DISABLED=1 npm run check`.

## Dane i synchronizacja

`../mocks` jest katalogiem `publicDir` Astro. Każde nagranie ma własny katalog:

```text
mocks/
  rick/
    rick.mp3
    transcript.json
  lovemenot/
    lovemenot.mp3
    transcript.json
```

Pliki są serwowane jako `/rick/rick.mp3`, `/rick/transcript.json`,
`/lovemenot/lovemenot.mp3` i `/lovemenot/transcript.json` (z uwzględnieniem
`BASE_URL`). Nie są kopiowane do źródeł frontendu; build umieszcza te katalogi
w `dist/`. Font Manrope jest dostarczany lokalnie wraz z aplikacją.

Plik `src/lib/tracks.ts` zawiera jedną listę nagrań z identyfikatorem, tytułem
i ścieżkami do audio oraz transkrypcji. Dodanie kolejnego nagrania wymaga
odpowiednich plików w `mocks/` i jednego wpisu na tej liście. Pierwszy wpis,
`rick`, jest domyślnym wyborem po odświeżeniu strony.

Odtwarzacz czyta kontrakt `schema_version: 1`, `text` i `words` z transkrypcji.
Każde słowo ma `text`, `start`, `end` w sekundach oraz opcjonalne `confidence`.
Metadane `source` nie służą do pobierania nagrania z AWS.

Wers kończy się po `.`, `!`, `?`, przed przerwą wynoszącą co najmniej 0,8 s lub po
8 słowach. Podział zachowuje oryginalne słowa i timestampy. Zawijanie wersu na
mniejszym ekranie nie zmienia synchronizacji.

Zegarem jest wyłącznie `audio.currentTime`. Wypełnienie słowa to ograniczone do
0–1 `(currentTime - start) / (end - start)`. `requestAnimationFrame` odczytuje czas
podczas odtwarzania; zdarzenia audio aktualizują widok po pauzie i przewijaniu.
Nie ma sztucznego timera ani automatycznego uruchamiania dźwięku.

Po zamontowaniu React odczytuje także aktualny stan elementu audio, dzięki czemu
metadane wczytane przed hydratacją nie giną. Długość nagrania jest odświeżana przy
zdarzeniach audio. Nieznany czas wyświetla się jako `—:—`; do jego ustalenia
przewijanie jest wyłączone, ale można rozpocząć odtwarzanie. Czas nie jest
zastępowany długością transkrypcji.

Początkowe przesunięcie synchronizacji wynosi 0 s. Dokładność podświetlania zależy
od timestampów ASR i zgodności początku MP3 z nagraniem użytym do transkrypcji.
W `rick` tekst kończy się przy 121,1 s, a audio trwa około 131,9 s.
W `lovemenot` tekst kończy się przy 207,55 s, a audio trwa około 218,7 s.
W końcówce obu nagrań żadne słowo nie jest aktywne.

## Obsługa

- Wybierz `rick` lub `lovemenot` w nagłówku. Zmiana zatrzymuje audio, zeruje czas
  i błędy, wznawia śledzenie tekstu oraz wczytuje właściwą transkrypcję. Głośność
  i wyciszenie zostają zachowane; nowe nagranie startuje dopiero po kliknięciu play.
- Kliknij play lub aktywuj go klawiszem Enter/Spacja, aby rozpocząć odsłuch.
- Kliknij wers, aby przejść do jego początku. Stan play/pauza zostaje zachowany.
- Przewijaj tekst kółkiem, gestem lub klawiaturą, aby zawiesić śledzenie.
  „Wróć do tekstu” ponownie centruje bieżący wers.
- Suwaki pozycji i głośności działają także klawiszami strzałek po ustawieniu
  fokusu. Tab przechodzi między kontrolkami, Enter/Spacja aktywują przyciski.
- Przyciski obok play przesuwają nagranie o 5 sekund.
- Preferencja `prefers-reduced-motion` wyłącza animacje i płynne przewijanie.

## Weryfikacja

Testy jednostkowe sprawdzają grupowanie i zachowanie wszystkich 331 słów `rick`
oraz 355 słów `lovemenot`,
granice przedziałów, przerwy, przewijanie w obie strony i koniec transkrypcji.
Testy komponentu w Happy DOM sprawdzają sterowanie audio, podświetlanie,
śledzenie tekstu, głośność oraz stany ładowania, błędu i pustej transkrypcji.
Obejmują też metadane dostępne przed zamontowaniem React, późne ustalenie długości,
nieznany czas, szybkie play/pauza i ponowne odtwarzanie po końcu nagrania.
Testy zmiany nagrania sprawdzają poprawne pary audio/tekst, zachowanie głośności
i wyciszenia, reset stanu oraz ignorowanie spóźnionych odpowiedzi i błędów
poprzedniego nagrania.
W testach komponentu API audio jest zastąpione atrapą.

Do sprawdzenia w rzeczywistej przeglądarce: odsłuch i zgodność tekstu w kilku
fragmentach, wygląd przy szerokościach 375 px i 1440 px, brak poziomego overflow,
pauza/wznowienie, przewijanie i przycisk powrotu do tekstu. Środowisko tworzenia
projektu nie udostępniało przeglądarki, więc kontroli wizualnej i odsłuchu nie
wykonano.
