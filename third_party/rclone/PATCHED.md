# rclone v1.75.2 (com patch do ad-manager)

Cópia podada de `github.com/rclone/rclone` v1.75.2 (licença MIT em `COPYING`): só os pacotes que o ad-manager
importa (`go list -deps ./internal/syncer | grep rclone/rclone`, sem testes), usada pelo `replace` do `go.mod` da
raiz — o motor de sync embutido (`internal/syncer`, docs/sync.md), sem o binário `rclone`.

Patch (procure `ad-manager patch`):

- `backend/smb/connpool.go` (`dial`): o dialer SMB é criado com `smb2.Negotiator{RequireMessageSigning: true}`.
  O rclone deixa a assinatura a critério do servidor; aqui toda conexão exige assinatura, e o `go-smb2` então
  também recusa sessão de convidado e anônima. Cifragem SMB3 é usada quando o servidor exige (o cliente não tem
  como exigir).

- `backend/smb/smb.go` (`List`): uma entrada que é ponto de reparse (link simbólico, junção) é pulada e registrada
  no log, nunca seguida: um link dentro da pasta de origem não traz outra árvore do servidor.

- `backend/smb/smb.go` (`NewFs`): nenhuma pasta do caminho de origem pode ser ponto de reparse; uma origem trocada
  por um link é recusada.
- `backend/smb/smb.go` (`Exists`, novo): diz se um nome existe na origem, link incluído, sem segui-lo — o espelho
  pergunta isto antes de apagar um arquivo do plano.

- `backend/smb/smb.go` (`List`, `Object.Open`): antes de ler uma pasta ou abrir um arquivo, confere sem seguir que
  não virou link depois da listagem (resta a janela entre a conferência e a abertura: o protocolo do go-smb2 não
  dá um "abrir sem seguir"). `Exists` confere cada pasta do caminho.

Atualizar: troque a versão no `go.mod` da raiz (sem o `replace`), copie de novo os pacotes da lista acima, reaplique
o patch e rode `go test ./internal/syncer/...` (com `SYNC_TEST_HOST` contra o Samba do Docker).
