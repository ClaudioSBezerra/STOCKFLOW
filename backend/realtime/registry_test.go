package realtime

import (
	"testing"
	"time"
)

// empresaTeste é a partição de Empresa usada pelos testes que não estão
// exercitando o isolamento em si (Story 9.1): o Registry passou a ser
// particionado por `empresaID`, então todo Publish/Subscribe precisa nomear
// uma — o comportamento DENTRO de uma partição é o de antes desta story.
const empresaTeste = "empresa-a"

// TestRegistry_PublishSubscribe prova o caminho feliz: um evento publicado
// num canal válido chega ao assinante, com `Resource` preenchido a partir
// do `canal` informado a Publish (nunca do `Evento` passado pelo chamador).
func TestRegistry_PublishSubscribe(t *testing.T) {
	r := NewRegistry()
	eventos, cancelar := r.Subscribe(empresaTeste)
	defer cancelar()

	r.Publish(empresaTeste, "produtos", Evento{ID: "produto-1", Change: "created"})

	select {
	case ev := <-eventos:
		if ev.Resource != "produtos" || ev.ID != "produto-1" || ev.Change != "created" {
			t.Fatalf("evento = %+v, want {produtos produto-1 created}", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("evento não chegou ao assinante em 1s")
	}
}

// TestRegistry_PublishCanalInvalidoPanica prova que um canal fora do
// conjunto fixo {produtos,estoques,movimentacoes,pedidos} faz Publish
// entrar em panic (fail-fast), sem alcançar nenhum assinante.
func TestRegistry_PublishCanalInvalidoPanica(t *testing.T) {
	r := NewRegistry()
	defer func() {
		if recover() == nil {
			t.Fatal("Publish(canal inválido) não entrou em panic")
		}
	}()
	r.Publish(empresaTeste, "nao-existe", Evento{ID: "x", Change: "created"})
}

// TestRegistry_MultiplosAssinantesRecebemOMesmoEvento prova o fan-out: os N
// assinantes atuais recebem o mesmo evento publicado uma única vez —
// nenhum canal dedicado por assinante, todos leem do mesmo Publish.
func TestRegistry_MultiplosAssinantesRecebemOMesmoEvento(t *testing.T) {
	r := NewRegistry()
	ch1, cancelar1 := r.Subscribe(empresaTeste)
	defer cancelar1()
	ch2, cancelar2 := r.Subscribe(empresaTeste)
	defer cancelar2()

	r.Publish(empresaTeste, "estoques", Evento{ID: "estoque-1", Change: "updated"})

	for i, ch := range []<-chan Evento{ch1, ch2} {
		select {
		case ev := <-ch:
			if ev.Resource != "estoques" || ev.ID != "estoque-1" {
				t.Fatalf("assinante %d: evento = %+v", i, ev)
			}
		case <-time.After(time.Second):
			t.Fatalf("assinante %d: evento não chegou em 1s", i)
		}
	}
}

// TestRegistry_AssinanteLentoNaoTravaOProdutor prova que Publish nunca
// bloqueia: um assinante cujo canal já está com o buffer cheio (nunca lido)
// não impede Publish de retornar — o evento extra é simplesmente perdido
// para ESSE assinante.
func TestRegistry_AssinanteLentoNaoTravaOProdutor(t *testing.T) {
	r := NewRegistry()
	_, cancelar := r.Subscribe(empresaTeste) // nunca lido — força o buffer a encher
	defer cancelar()

	done := make(chan struct{})
	go func() {
		for i := 0; i < eventoBufferSize+5; i++ {
			r.Publish(empresaTeste, "produtos", Evento{ID: "x", Change: "updated"})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish bloqueou com assinante lento — devia ser não-bloqueante")
	}
}

// TestRegistry_SubscribeAposCancelarNaoRecebeMais prova que a função de
// cancelamento devolvida por Subscribe desinscreve de verdade: um Publish
// posterior ao cancelamento não chega mais nesse canal (e o cancelamento é
// seguro para chamar mais de uma vez).
func TestRegistry_SubscribeAposCancelarNaoRecebeMais(t *testing.T) {
	r := NewRegistry()
	eventos, cancelar := r.Subscribe(empresaTeste)
	cancelar()
	cancelar() // idempotente — não deve panicar (double close)

	r.Publish(empresaTeste, "pedidos", Evento{ID: "y", Change: "created"})

	select {
	case ev, ok := <-eventos:
		if ok {
			t.Fatalf("recebeu evento %+v após cancelar — assinante deveria estar desinscrito", ev)
		}
		// canal fechado (ok=false) é o resultado esperado do cancelamento.
	case <-time.After(200 * time.Millisecond):
		t.Fatal("canal não foi fechado pelo cancelamento")
	}
}

// TestRegistry_EventoNaoCruzaEmpresa é o teste-critério do particionamento
// por Empresa (Story 9.1, spec-9-1): um evento publicado para a Empresa A
// chega ao assinante da Empresa A e NUNCA ao da Empresa B — antes desta
// story todo assinante do processo recebia todo evento.
func TestRegistry_EventoNaoCruzaEmpresa(t *testing.T) {
	r := NewRegistry()
	chA, cancelarA := r.Subscribe("empresa-a")
	defer cancelarA()
	chB, cancelarB := r.Subscribe("empresa-b")
	defer cancelarB()

	r.Publish("empresa-a", "produtos", Evento{ID: "produto-da-a", Change: "created"})

	select {
	case ev := <-chA:
		if ev.ID != "produto-da-a" {
			t.Fatalf("assinante da empresa A: evento = %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("assinante da empresa A não recebeu o evento da própria empresa")
	}

	select {
	case ev := <-chB:
		t.Fatalf("assinante da empresa B recebeu evento da empresa A: %+v", ev)
	case <-time.After(200 * time.Millisecond):
		// nenhum evento — é o resultado esperado.
	}
}

// TestRegistry_PublishParaEmpresaSemAssinanteEhNoOp prova que publicar para
// uma Empresa sem nenhuma conexão aberta não entra em panic nem afeta as
// outras partições (o mapa simplesmente não tem aquela chave).
func TestRegistry_PublishParaEmpresaSemAssinanteEhNoOp(t *testing.T) {
	r := NewRegistry()
	ch, cancelar := r.Subscribe("empresa-a")
	defer cancelar()

	r.Publish("empresa-sem-ninguem", "pedidos", Evento{ID: "z", Change: "created"})

	select {
	case ev := <-ch:
		t.Fatalf("assinante da empresa A recebeu evento de outra empresa: %+v", ev)
	case <-time.After(200 * time.Millisecond):
	}
}
