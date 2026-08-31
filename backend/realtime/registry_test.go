package realtime

import (
	"testing"
	"time"
)

// TestRegistry_PublishSubscribe prova o caminho feliz: um evento publicado
// num canal válido chega ao assinante, com `Resource` preenchido a partir
// do `canal` informado a Publish (nunca do `Evento` passado pelo chamador).
func TestRegistry_PublishSubscribe(t *testing.T) {
	r := NewRegistry()
	eventos, cancelar := r.Subscribe()
	defer cancelar()

	r.Publish("produtos", Evento{ID: "produto-1", Change: "created"})

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
	r.Publish("nao-existe", Evento{ID: "x", Change: "created"})
}

// TestRegistry_MultiplosAssinantesRecebemOMesmoEvento prova o fan-out: os N
// assinantes atuais recebem o mesmo evento publicado uma única vez —
// nenhum canal dedicado por assinante, todos leem do mesmo Publish.
func TestRegistry_MultiplosAssinantesRecebemOMesmoEvento(t *testing.T) {
	r := NewRegistry()
	ch1, cancelar1 := r.Subscribe()
	defer cancelar1()
	ch2, cancelar2 := r.Subscribe()
	defer cancelar2()

	r.Publish("estoques", Evento{ID: "estoque-1", Change: "updated"})

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
	_, cancelar := r.Subscribe() // nunca lido — força o buffer a encher
	defer cancelar()

	done := make(chan struct{})
	go func() {
		for i := 0; i < eventoBufferSize+5; i++ {
			r.Publish("produtos", Evento{ID: "x", Change: "updated"})
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
	eventos, cancelar := r.Subscribe()
	cancelar()
	cancelar() // idempotente — não deve panicar (double close)

	r.Publish("pedidos", Evento{ID: "y", Change: "created"})

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
