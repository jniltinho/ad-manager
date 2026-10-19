// pt-BR for the OUs page.
export default {
  'Organizational units and default containers of': 'Unidades organizacionais e containers padrão de',
  'New OU': 'Nova OU',
  'OU {name} created.': 'OU {name} criada.',
  'OU {name} deleted.': 'OU {name} excluída.',
  'Delete the OU {name}? Only possible when it is empty.': 'Excluir a OU {name}? Só é possível se ela estiver vazia.',
  Inside: 'Dentro de',
  'domain root': 'raiz do domínio',
  // GPOs (read only)
  'Group Policy Objects (GPOs)': 'Objetos de Diretiva de Grupo (GPOs)',
  'Version (computer / user)': 'Versão (computador / usuário)',
  'Linked to': 'Vinculada a',
  Extensions: 'Extensões',
  'in use': 'em uso',
  'turned off': 'desabilitada',
  'user part disabled': 'parte de usuário desabilitada',
  'computer part disabled': 'parte de computador desabilitada',
  'not linked': 'sem vínculo',
  enforced: 'imposta',
  'No GPOs.': 'Nenhuma GPO.',
  'Inheritance blocked in': 'Herança bloqueada em',
  'From the directory (LDAP). A struck-through link is disabled; in a container, link order 1 wins.':
    'Do diretório (LDAP). Vínculo riscado está desabilitado; num contêiner, a ordem de vínculo 1 prevalece.',
  backup: 'backup',
  'Back up this GPO (needs the agent installed on a domain controller)':
    'Fazer backup desta GPO (precisa do agente instalado num controlador de domínio)',
  'Create the new GPO {name} from {file}? It is created unlinked, with a new GUID.':
    'Criar a nova GPO {name} a partir de {file}? Ela é criada sem vínculo, com um GUID novo.',
  'GPO {name} created ({guid}).': 'GPO {name} criada ({guid}).',
  'Samba does not replicate SYSVOL: copy the folder Policies/{guid} from {dc} to {dcs} before linking the GPO.':
    'O Samba não replica o SYSVOL: copie a pasta Policies/{guid} de {dc} para {dcs} antes de vincular a GPO.',
  'Restore a backup as a new GPO': 'Restaurar um backup como GPO nova',
  'Backup file': 'Arquivo de backup',
  'Name of the new GPO': 'Nome da nova GPO',
  'Needs the agent on a domain controller in its GPO restore mode (gpo_restore_user). The new GPO is not linked.':
    'Precisa do agente num controlador de domínio no modo de restauração de GPO (gpo_restore_user). A nova GPO não é vinculada.',
}
