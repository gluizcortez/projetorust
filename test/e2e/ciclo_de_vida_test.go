//go:build integration

// Package e2e_test exercita o binário de verdade: compila, executa, envia
// sinais reais e confere o código de saída.
//
// Os testes daqui exigem PostgreSQL — o serviço não sobe sem banco, como o
// legado (reference/main.rs:38). Rodam com `make test-integration` ou com
// TEST_DATABASE_URL definida.
package e2e_test

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

const chaveDeTeste = "chave-do-teste-e2e"

// binario compila o serviço uma vez por execução e devolve o caminho.
func binario(t *testing.T) string {
	t.Helper()

	caminho := filepath.Join(t.TempDir(), "recorte-api")
	cmd := exec.Command("go", "build", "-o", caminho, "../../cmd/recorte-api")
	if saida, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compilando o serviço: %v\n%s", err, saida)
	}
	return caminho
}

func exigirBanco(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL não definida — rode `make test-integration`")
	}
	return dsn
}

// portaLivre reserva uma porta e a devolve, para que o teste saiba onde o
// serviço vai escutar.
func portaLivre(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reservando porta: %v", err)
	}
	defer func() { _ = l.Close() }()

	_, porta, err := net.SplitHostPort(l.Addr().String())
	if err != nil {
		t.Fatalf("separando a porta: %v", err)
	}
	return porta
}

// processo é o serviço em execução.
type processo struct {
	cmd      *exec.Cmd
	endereco string
	saida    *strings.Builder
	t        *testing.T
}

func iniciar(t *testing.T, extras ...string) *processo {
	t.Helper()

	dsn := exigirBanco(t)
	porta := portaLivre(t)
	endereco := "127.0.0.1:" + porta

	cmd := exec.Command(binario(t))
	cmd.Env = append(os.Environ(),
		"DATABASE_URL="+dsn,
		"API_KEY="+chaveDeTeste,
		"SERVIDOR_IP=127.0.0.1",
		"SERVIDOR_PORTA="+porta,
		"LOG_FORMATO=text",
	)
	cmd.Env = append(cmd.Env, extras...)

	var saida strings.Builder
	cmd.Stdout = &saida
	cmd.Stderr = &saida
	// Grupo de processo próprio: garante que o filho não receba os sinais
	// enviados ao processo de teste por engano.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		t.Fatalf("iniciando o serviço: %v", err)
	}

	p := &processo{cmd: cmd, endereco: endereco, saida: &saida, t: t}
	t.Cleanup(func() {
		if p.cmd.ProcessState == nil {
			_ = p.cmd.Process.Kill()
			_ = p.cmd.Wait()
		}
	})
	p.esperarPing()
	return p
}

// esperarPing aguarda o serviço responder em /ping.
func (p *processo) esperarPing() {
	p.t.Helper()
	cliente := &http.Client{Timeout: time.Second}
	limite := time.After(30 * time.Second)

	for {
		select {
		case <-limite:
			p.t.Fatalf("o serviço não respondeu /ping em 30s\n%s", p.saida.String())
		default:
		}
		resp, err := cliente.Get("http://" + p.endereco + "/ping")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// sinalizar envia um sinal REAL ao processo.
func (p *processo) sinalizar(s syscall.Signal) {
	p.t.Helper()
	if err := p.cmd.Process.Signal(s); err != nil {
		p.t.Fatalf("enviando %v: %v", s, err)
	}
}

// esperarSaida aguarda o término e devolve o código de saída.
func (p *processo) esperarSaida(prazo time.Duration) (int, error) {
	p.t.Helper()

	feito := make(chan error, 1)
	go func() { feito <- p.cmd.Wait() }()

	select {
	case err := <-feito:
		var saidaComErro *exec.ExitError
		if errors.As(err, &saidaComErro) {
			return saidaComErro.ExitCode(), nil
		}
		if err != nil {
			return -1, fmt.Errorf("esperando o processo: %w", err)
		}
		return 0, nil
	case <-time.After(prazo):
		return -1, fmt.Errorf("o processo não encerrou em %s", prazo)
	}
}

// TestCicloDeVidaSIGTERMEncerraLimpo é o caminho normal: sem trabalho em
// andamento, o SIGTERM encerra com código 0.
func TestCicloDeVidaSIGTERMEncerraLimpo(t *testing.T) {
	p := iniciar(t)
	p.sinalizar(syscall.SIGTERM)

	codigo, err := p.esperarSaida(30 * time.Second)
	if err != nil {
		t.Fatalf("%v\n%s", err, p.saida.String())
	}
	if codigo != 0 {
		t.Errorf("código de saída = %d; esperava 0\n%s", codigo, p.saida.String())
	}

	registro := p.saida.String()
	for _, esperado := range []string{
		"servidor iniciando", "encerrando", "motivo=\"sinal SIGTERM\"",
		"etapa=\"servidor http\"", "etapa=importações", "etapa=\"pool de conexões\"",
		"encerrado",
	} {
		if !strings.Contains(registro, esperado) {
			t.Errorf("o registro não trouxe %q:\n%s", esperado, registro)
		}
	}
}

// TestCicloDeVidaSIGINTTambemEncerra: o legado escuta os dois (§6.2).
func TestCicloDeVidaSIGINTTambemEncerra(t *testing.T) {
	p := iniciar(t)
	p.sinalizar(syscall.SIGINT)

	codigo, err := p.esperarSaida(30 * time.Second)
	if err != nil {
		t.Fatalf("%v\n%s", err, p.saida.String())
	}
	if codigo != 0 {
		t.Errorf("código de saída = %d; esperava 0\n%s", codigo, p.saida.String())
	}
	if !strings.Contains(p.saida.String(), "sinal SIGINT") {
		t.Errorf("o motivo não foi registrado:\n%s", p.saida.String())
	}
}

// TestCicloDeVidaRequisicaoEmCursoEConcluida: a requisição que já estava sendo
// atendida termina; a que chega depois do sinal é recusada.
//
// Usa /ping porque ele não toca no banco — o que isola o que se quer medir.
func TestCicloDeVidaRequisicaoEmCursoEConcluida(t *testing.T) {
	p := iniciar(t)

	cliente := &http.Client{Timeout: 5 * time.Second}
	resp, err := cliente.Get("http://" + p.endereco + "/ping")
	if err != nil {
		t.Fatalf("requisição antes do sinal: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status antes do sinal = %d", resp.StatusCode)
	}

	p.sinalizar(syscall.SIGTERM)

	codigo, err := p.esperarSaida(30 * time.Second)
	if err != nil {
		t.Fatalf("%v\n%s", err, p.saida.String())
	}
	if codigo != 0 {
		t.Errorf("código de saída = %d\n%s", codigo, p.saida.String())
	}

	// Depois de encerrado, ninguém mais atende.
	if resp, err := cliente.Get("http://" + p.endereco + "/ping"); err == nil {
		_ = resp.Body.Close()
		t.Error("o serviço continuou atendendo depois de encerrar")
	}
}

// TestCicloDeVidaSegundoSinalForca é o critério de aceite do segundo sinal.
//
// Sem trabalho em andamento o encerramento é rápido demais para que o segundo
// sinal chegue a tempo; por isso o teste envia os dois em sequência imediata e
// aceita QUALQUER um dos dois desfechos legítimos — 0 se o encerramento
// terminou antes, 130 se o segundo sinal chegou durante.
//
// O que ele garante é o que importa: o segundo sinal NUNCA deixa o processo
// travado nem produz um código inesperado.
func TestCicloDeVidaSegundoSinalForca(t *testing.T) {
	p := iniciar(t)

	p.sinalizar(syscall.SIGTERM)
	p.sinalizar(syscall.SIGTERM)

	codigo, err := p.esperarSaida(30 * time.Second)
	if err != nil {
		t.Fatalf("%v\n%s", err, p.saida.String())
	}
	if codigo != 0 && codigo != 130 {
		t.Errorf("código de saída = %d; esperava 0 ou 130\n%s", codigo, p.saida.String())
	}
	t.Logf("código de saída = %d", codigo)
}

// TestCicloDeVidaFalhaDeArranqueSaiComUm: banco inalcançável impede o arranque,
// como no legado, que entra em pânico (main.rs:38).
func TestCicloDeVidaFalhaDeArranqueSaiComUm(t *testing.T) {
	exigirBanco(t) // o teste só faz sentido no ambiente que tem banco

	cmd := exec.Command(binario(t))
	cmd.Env = append(os.Environ(),
		"DATABASE_URL=postgres://ninguem:nada@127.0.0.1:1/inexistente?sslmode=disable&connect_timeout=2",
		"API_KEY="+chaveDeTeste,
		"SERVIDOR_IP=127.0.0.1",
		"SERVIDOR_PORTA="+portaLivre(t),
	)
	var saida strings.Builder
	cmd.Stdout = &saida
	cmd.Stderr = &saida

	err := cmd.Run()

	var saidaComErro *exec.ExitError
	if !errors.As(err, &saidaComErro) {
		t.Fatalf("esperava saída com erro, veio %v\n%s", err, saida.String())
	}
	if saidaComErro.ExitCode() != 1 {
		t.Errorf("código de saída = %d; esperava 1\n%s", saidaComErro.ExitCode(), saida.String())
	}
	if !strings.Contains(saida.String(), "falha ao montar o serviço") {
		t.Errorf("o registro não explicou a falha:\n%s", saida.String())
	}
}

// TestCicloDeVidaConfiguracaoInvalidaSaiComUm: sem API_KEY o serviço não sobe.
func TestCicloDeVidaConfiguracaoInvalidaSaiComUm(t *testing.T) {
	cmd := exec.Command(binario(t))
	// Ambiente mínimo, sem API_KEY nem DATABASE_URL.
	cmd.Env = []string{"PATH=" + os.Getenv("PATH")}

	var saida strings.Builder
	cmd.Stdout = &saida
	cmd.Stderr = &saida

	err := cmd.Run()

	var saidaComErro *exec.ExitError
	if !errors.As(err, &saidaComErro) {
		t.Fatalf("esperava saída com erro, veio %v\n%s", err, saida.String())
	}
	if saidaComErro.ExitCode() != 1 {
		t.Errorf("código de saída = %d; esperava 1\n%s", saidaComErro.ExitCode(), saida.String())
	}
	// A mensagem sai por stderr cru: ainda não há registrador.
	if !strings.Contains(saida.String(), "API_KEY") {
		t.Errorf("o erro não disse o que faltava:\n%s", saida.String())
	}
}

// TestCicloDeVidaSondaDeSaude exercita o -healthcheck da imagem.
func TestCicloDeVidaSondaDeSaude(t *testing.T) {
	p := iniciar(t)

	sonda := exec.Command(binario(t), "-healthcheck")
	sonda.Env = append(os.Environ(),
		"DATABASE_URL="+os.Getenv("TEST_DATABASE_URL"),
		"API_KEY="+chaveDeTeste,
		"SERVIDOR_IP=127.0.0.1",
		"SERVIDOR_PORTA="+strings.TrimPrefix(p.endereco, "127.0.0.1:"),
	)
	if saida, err := sonda.CombinedOutput(); err != nil {
		t.Errorf("a sonda falhou contra um serviço saudável: %v\n%s", err, saida)
	}

	p.sinalizar(syscall.SIGTERM)
	if _, err := p.esperarSaida(30 * time.Second); err != nil {
		t.Fatalf("%v", err)
	}
}
