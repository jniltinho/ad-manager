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
