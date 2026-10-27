// The desktop app's own strings (src/desktop) and its Go messages (desktop-wails/): loaded only by the desktop build.
export default {
  Configure: 'Configurar',
  'Panel URL': 'URL do painel',
  'Test connection': 'Testar conexão',
  'Testing…': 'Testando…',
  'Signs in with this account; the password is never saved.': 'Entra com esta conta; a senha nunca é salva.',
  'Connected to {domain} as {user} (panel {version}).': 'Conectado a {domain} como {user} (painel {version}).',
  'Certificate not trusted': 'Certificado não confiável',
  'Compare the fingerprint with the one the panel logs at start (journalctl -u ad-manager | grep sha256).':
    'Compare a impressão digital com a que o painel registra ao iniciar (journalctl -u ad-manager | grep sha256).',
  Fingerprint: 'Impressão digital',
  Subject: 'Assunto',
  Issuer: 'Emissor',
  Valid: 'Validade',
  'Trust this certificate': 'Confiar neste certificado',
  'This computer': 'Este computador',
  Language: 'Idioma',
  'Panel default': 'Padrão do painel',
  Look: 'Aparência',
  Flat: 'Plano',
  'With depth': 'Com profundidade',
  'Safe rendering (GPU off): for a graphics driver that cannot draw the window. Applies at the next start.':
    'Renderização segura (sem GPU): para um driver de vídeo que não consegue desenhar a janela. Vale no próximo início.',
  'Without a new test, Save keeps the panel and saves only this computer’s preferences.':
    'Sem um novo teste, Salvar mantém o painel e salva só as preferências deste computador.',
  '{path} must be private and has mode {mode}: run chmod 600': '{path} precisa ser privado e tem modo {mode}: rode chmod 600',
  'Send asks for the file.': 'Enviar pede o arquivo.',
  // Go messages (desktop-wails/)
  'Panel URL: enter an https://server:port URL': 'URL do painel: informe uma URL https://servidor:porta',
  'the panel answered with a redirect, which is not followed: check the panel URL':
    'o painel respondeu com um redirecionamento, que não é seguido: confira a URL do painel',
  "the panel's certificate is not the one trusted in Configure (it changed): check it with the panel's administrator, then trust it again in Configure":
    'o certificado do painel não é o confiado em Configurar (mudou): confira com o administrador do painel e confie de novo em Configurar',
  'the panel is being set up: finish its setup wizard in a browser first':
    'o painel está sendo configurado: termine o assistente de configuração num navegador primeiro',
  'the panel is not configured: open Configure': 'o painel não está configurado: abra Configurar',
  'test the connection before saving': 'teste a conexão antes de salvar',
  "only the panel's /asynqmon and /swagger pages open outside the app": 'só as páginas /asynqmon e /swagger do painel abrem fora do app',
  'larger than 32 MiB, the most the desktop app passes through its window at once':
    'maior que 32 MiB, o máximo que o app desktop passa pela janela de uma vez',
  'File-server tokens are no longer used: they were removed from the configuration file.':
    'Tokens de servidores de arquivos não são mais usados: foram removidos do arquivo de configuração.',
  'the panel presented no certificate': 'o painel não apresentou certificado',
}
