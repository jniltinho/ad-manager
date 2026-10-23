## ADDED Requirements

### Requirement: Inheritance notes of a sync run translated
The server catalog SHALL translate into pt-BR every note the sync run writes about the Windows-inheritance pass:
applied, not applied, the incremental pass failing back to the whole folder, a pending inheritance being repaired,
and a pending mark that could not be written.

#### Scenario: Repaired pending inheritance in Portuguese
- **WHEN** a pt-BR panel shows a run noting "an earlier run left the Windows inheritance pending: reapplying it to
  the whole folder"
- **THEN** it reads "uma execução anterior deixou a herança do Windows pendente: reaplicando-a na pasta inteira"
