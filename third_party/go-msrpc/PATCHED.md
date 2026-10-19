# go-msrpc v1.6.4 (com patch do ad-manager)

Cópia podada de `github.com/oiweiwei/go-msrpc` v1.6.4 (licença em `LICENSE`): só os pacotes que o ad-manager
importa (`go list -deps ./... | grep go-msrpc`), usada pelo `replace` do `go.mod` da raiz.

Patch (procure `ad-manager patch`):

- `dcerpc/packet.go`: um PDU `fault` com status 0 era decodificado como resposta, sem passar pela verificação de
  assinatura (que só cobre `request`/`response`); um host no meio poderia forjar uma resposta de sucesso numa
  conexão selada. Agora é erro (`ErrFaultWithoutStatus`).
- `dcerpc/client_conn.go` (`Invoke`): a resposta de uma chamada aceitava qualquer PDU decodificado — um
  `bind_ack` ou `alter_context_resp` (fora da verificação) com o `call_id` certo preencheria a saída da operação.
  Agora só `response` (cuja assinatura o `Unwrap` verificou); o resto é `ErrUnexpectedPDU`, recusado em
  `readPacket` antes de decodificar o stub (nenhuma desserialização de dado não verificado), e de novo no `Invoke`.
- `dcerpc/client_conn.go` (`Unwrap`): um `response` com a flag `PFC_OBJECT_UUID` deslocava 16 bytes a faixa
  verificada em relação ao stub decodificado (um prefixo não verificado viraria resultado). Resposta nunca traz
  UUID de objeto: recusada (`ErrUnexpectedPDU`).

- `dcerpc/client_conn.go` (`invoke`) e `dcerpc/packet.go`: sem assinatura do cabeçalho o `auth_pad_length` não
  é autenticado e podia cortar o stub — um `Return` ausente virava 0 (sucesso). Agora o pad passa de 15 é erro e,
  no último fragmento, a saída da operação precisa estar completa (`ErrTruncatedResponse`); teste comportamental
  em `dcerpc/patch_test.go` (`make test` e CI).

Para atualizar: copie de novo os pacotes da versão nova (mesmo comando `go list -deps`), reaplique o patch se o
upstream não o tiver incorporado e rode `go test ./internal/winrpc` (o teste `TestMsrpcPatched` confere o patch).
