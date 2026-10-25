# language Specification

## Purpose
Interface and messages authored in English with a pt-BR translation, with the language chosen per user and on the server.

## Requirements

### Requirement: Interface in English and Portuguese
Every panel screen SHALL exist in English (en) and Portuguese (pt-BR). The initial language is the one the operator
chose in this browser; without a choice, the default language from Settings, which is English (`en`) out of the box.

#### Scenario: Switch to Portuguese
- **WHEN** the operator chooses Portuguese (`pt-BR`) in the language switcher
- **THEN** menus, buttons, columns, dialogs and notices appear in Portuguese, and the choice persists on reload

#### Scenario: Organization default
- **WHEN** the default language is Portuguese and the browser has never chosen a language
- **THEN** the login and the panel open in Portuguese

### Requirement: Inheritance notes of a sync run translated
The server catalog SHALL translate into pt-BR every note the sync run writes about the Windows-inheritance pass:
applied, not applied, the incremental pass failing back to the whole folder, a pending inheritance being repaired,
and a pending mark that could not be written.

#### Scenario: Repaired pending inheritance in Portuguese
- **WHEN** a pt-BR panel shows a run noting "an earlier run left the Windows inheritance pending: reapplying it to
  the whole folder"
- **THEN** it reads "uma execução anterior deixou a herança do Windows pendente: reaplicando-a na pasta inteira"
