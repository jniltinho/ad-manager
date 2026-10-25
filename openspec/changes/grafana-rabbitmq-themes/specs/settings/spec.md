## ADDED Requirements

### Requirement: Grafana and RabbitMQ themes
The theme registry SHALL include `grafana` (dark: the Grafana 11 dashboard, flat panels on the dark canvas, no
gradients or depth shadows) and `rabbitmq` (light: the RabbitMQ management UI, white header with the orange rule,
orange current tab, Verdana), on the web and on the desktop, each passing the contrast matrix (normal text 4.5:1,
large text and components 3:1).

#### Scenario: Choose Grafana
- **WHEN** the administrator chooses "Grafana (dark)" and saves
- **THEN** `brand.theme` is `grafana`, the panel, the login and the desktop app turn dark with flat panels

#### Scenario: Choose RabbitMQ
- **WHEN** the administrator chooses "RabbitMQ" and saves
- **THEN** `brand.theme` is `rabbitmq`, the header is white with an orange rule and the current tab is orange, on the
  web and on the desktop

#### Scenario: Contrast
- **WHEN** the contrast tests run for `grafana` and `rabbitmq`
- **THEN** every recipe passes with no pending pair

### Requirement: App outline
Every theme SHALL draw a 1 px line in its `color-line` around the app box, on the web and on the desktop, in the
normal and the flat look, so the app's edge shows against the page.

#### Scenario: Flat RabbitMQ
- **WHEN** the RabbitMQ theme and the flat look are active
- **THEN** a thin line marks the app box's edge, and the page is darker than the header

### Requirement: Queue monitor in the panel's theme
`/asynqmon` SHALL serve the Vue frontend of `github.com/jniltinho/asynqmon`, only to a signed-in administrator, opened
in the panel's current theme and flat look.

#### Scenario: Theme follows the panel
- **WHEN** the administrator saves the RabbitMQ theme and opens Queues
- **THEN** `/asynqmon` loads with `data-theme="rabbitmq"` and the panel's flat setting, with no theme flash
