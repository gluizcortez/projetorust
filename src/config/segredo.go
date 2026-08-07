package config

import (
	"fmt"
	"log/slog"
	"net/url"
	"strings"
)

// MarcadorRedigido substitui todo valor sensível em qualquer saída textual.
//
// É um literal fixo, nunca um prefixo nem um resumo do valor real: revelar os
// primeiros caracteres de uma credencial é revelar parte dela.
//
// Usa apenas ASCII de propósito: o marcador entra em URLs que passam por
// url.String(), e caracteres fora do ASCII sairiam percent-encoded — legíveis,
// mas ruidosos no diagnóstico.
const MarcadorRedigido = "REDIGIDO"

// Segredo é um valor sensível que nunca aparece em texto formatado.
//
// A proteção é estrutural, não disciplinar: quem escrever fmt.Sprintf("%v", s),
// slog.Any("chave", s), %#v ou json.Marshal obtém o marcador. O valor real só
// sai por Revelar, que é fácil de auditar por busca textual.
type Segredo string

// String satisfaz fmt.Stringer e cobre %v e %s.
func (s Segredo) String() string { return MarcadorRedigido }

// GoString cobre %#v.
func (s Segredo) GoString() string { return MarcadorRedigido }

// LogValue satisfaz slog.LogValuer e cobre o registro estruturado.
func (s Segredo) LogValue() slog.Value { return slog.StringValue(MarcadorRedigido) }

// MarshalText cobre encoding/json e demais codificadores textuais.
func (s Segredo) MarshalText() ([]byte, error) { return []byte(MarcadorRedigido), nil }

// Revelar devolve o valor real. É o único caminho para ele.
func (s Segredo) Revelar() string { return string(s) }

// Vazio informa se o segredo não foi fornecido.
func (s Segredo) Vazio() bool { return string(s) == "" }

// URLSegredo é uma URL de conexão cuja credencial é redigida, preservando o
// que é útil em diagnóstico: esquema, host, porta e nome da base.
type URLSegredo string

// String devolve a URL sem a credencial.
func (u URLSegredo) String() string { return redigirURL(string(u)) }

// GoString cobre %#v.
func (u URLSegredo) GoString() string { return redigirURL(string(u)) }

// LogValue satisfaz slog.LogValuer.
func (u URLSegredo) LogValue() slog.Value { return slog.StringValue(redigirURL(string(u))) }

// MarshalText cobre encoding/json.
func (u URLSegredo) MarshalText() ([]byte, error) { return []byte(redigirURL(string(u))), nil }

// Revelar devolve a URL completa, com credencial. É o único caminho para ela.
func (u URLSegredo) Revelar() string { return string(u) }

// Vazio informa se a URL não foi fornecida.
func (u URLSegredo) Vazio() bool { return string(u) == "" }

// parametrosSensiveis são chaves de consulta que carregam credencial em URLs
// de conexão do PostgreSQL.
var parametrosSensiveis = map[string]bool{
	"password":     true,
	"passwd":       true,
	"pwd":          true,
	"sslpassword":  true,
	"sslkey":       true,
	"sslcert":      true,
	"sslrootcert":  true,
	"krbsrvname":   true,
	"gsslib":       true,
	"passfile":     true,
	"requirepeer":  true,
	"service":      true,
	"target_sessi": true,
}

// redigirURL remove a credencial de uma URL de conexão, mantendo esquema, host,
// porta e caminho.
//
// Se a URL não puder ser analisada, devolve o marcador opaco: é preferível
// perder informação de diagnóstico a ecoar uma cadeia que pode conter a senha
// em posição inesperada.
func redigirURL(bruto string) string {
	if bruto == "" {
		return ""
	}

	u, err := url.Parse(bruto)
	if err != nil || u.Host == "" {
		return MarcadorRedigido
	}

	if u.User != nil {
		if _, temSenha := u.User.Password(); temSenha {
			u.User = url.UserPassword(u.User.Username(), MarcadorRedigido)
		} else {
			u.User = url.User(u.User.Username())
		}
	}

	if consulta := u.Query(); len(consulta) > 0 {
		alterou := false
		for chave := range consulta {
			if parametrosSensiveis[strings.ToLower(chave)] {
				consulta.Set(chave, MarcadorRedigido)
				alterou = true
			}
		}
		if alterou {
			u.RawQuery = consulta.Encode()
		}
	}

	return u.String()
}

// descreverPara produz uma linha legível de diagnóstico sem revelar segredo.
func descreverPara(nome string, valor fmt.Stringer) string {
	return nome + "=" + valor.String()
}
