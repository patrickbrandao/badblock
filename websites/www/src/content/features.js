// Conteúdo da página inicial: 6 seções e 33 cards.
// Edite textos, ordem, hues e ícones aqui.
// icon: { kind: 'feather', name } (src/components/icons.jsx) ou { kind: 'custom', name } (src/components/badges.jsx).
export const sections = [
  {
    "id": "home",
    "imgLight": "/landing-2/features/illustration-01-deploy--light.svg",
    "imgDark": "/landing-2/features/illustration-01-deploy--dark.svg",
    "alt": "Implantar",
    "title": "Na nuvem ou self-hosted",
    "sub": "A pilha BadBlock roda no site ou você pode usar o projeto publicado no Github e Docker Hub para rodar localmente no seu servidor, fazer personalizações e adicionar coletores e APIs privadas.",
    "cards": [
      {
        "icon": {
          "kind": "feather",
          "name": "box"
        },
        "hue": 322,
        "title": "Serviços",
        "desc": "Implante qualquer imagem de container como um serviço: basta indicar a origem.",
        "bullets": [
          {
            "icon": {
              "kind": "feather",
              "name": "box"
            },
            "text": "Imagem Docker"
          },
          {
            "icon": {
              "kind": "custom",
              "name": "custom-eb547990"
            },
            "text": "Repositório no GitHub"
          },
          {
            "icon": {
              "kind": "feather",
              "name": "terminal"
            },
            "text": "Repositório local"
          }
        ]
      },
      {
        "icon": {
          "kind": "feather",
          "name": "database"
        },
        "hue": 322,
        "title": "Bancos de dados",
        "desc": "Suba qualquer banco de dados, com backups integrados.",
        "bullets": [
          {
            "icon": {
              "kind": "custom",
              "name": "custom-019083ba"
            },
            "text": "PostgreSQL"
          },
          {
            "icon": {
              "kind": "custom",
              "name": "custom-d5c96e83"
            },
            "text": "MySQL"
          },
          {
            "icon": {
              "kind": "custom",
              "name": "custom-798f0ef7"
            },
            "text": "MongoDB"
          },
          {
            "icon": {
              "kind": "custom",
              "name": "custom-ea5665d8"
            },
            "text": "Redis"
          }
        ]
      },
      {
        "icon": {
          "kind": "feather",
          "name": "hard-drive"
        },
        "hue": 322,
        "title": "Volumes",
        "desc": "Anexe e monte volumes de armazenamento persistente de alto desempenho.",
        "bullets": [
          {
            "icon": {
              "kind": "feather",
              "name": "archive"
            },
            "text": "Até 5 TB de armazenamento"
          },
          {
            "icon": {
              "kind": "custom",
              "name": "custom-c31a4fcc"
            },
            "text": "Mais de 100.000 IOPS"
          },
          {
            "icon": {
              "kind": "feather",
              "name": "bar-chart-2"
            },
            "text": "Métricas de uso de disco"
          }
        ]
      },
      {
        "icon": {
          "kind": "feather",
          "name": "calendar"
        },
        "hue": 322,
        "title": "Tarefas agendadas (cron)",
        "desc": "Configure uma tarefa para rodar em horários fixos.",
        "bullets": [
          {
            "icon": {
              "kind": "feather",
              "name": "watch"
            },
            "text": "Intervalos a partir de 5 minutos"
          },
          {
            "icon": {
              "kind": "custom",
              "name": "custom-f9208c70"
            },
            "text": "Programável por expressão crontab"
          }
        ]
      },
      {
        "icon": {
          "kind": "feather",
          "name": "search"
        },
        "hue": 322,
        "title": "Templates",
        "desc": "Implante conjuntos de serviços, bancos de dados etc., tão complexos quanto precisar.",
        "bullets": [
          {
            "icon": {
              "kind": "feather",
              "name": "box"
            },
            "text": "Mais de 2.000 templates"
          },
          {
            "icon": {
              "kind": "feather",
              "name": "share"
            },
            "text": "Compartilháveis"
          },
          {
            "icon": {
              "kind": "custom",
              "name": "custom-a5622aa0"
            },
            "text": "Personalizáveis"
          }
        ]
      },
      {
        "icon": {
          "kind": "custom",
          "name": "custom-31f505b4"
        },
        "hue": 322,
        "title": "Variáveis",
        "desc": "Gerencie segredos e variáveis de ambiente em toda a stack.",
        "bullets": [
          {
            "icon": {
              "kind": "custom",
              "name": "custom-31f505b4"
            },
            "text": "Variáveis de serviço"
          },
          {
            "icon": {
              "kind": "custom",
              "name": "custom-31f505b4"
            },
            "text": "Variáveis compartilhadas"
          },
          {
            "icon": {
              "kind": "custom",
              "name": "custom-31f505b4"
            },
            "text": "Variáveis de referência"
          }
        ]
      }
    ],
    "logoRow": {
      "label": "Tecnologias",
      "logos": [
        {
          "src": "/landing-2/logos/comp/logo-docker.svg",
          "alt": "logo do Docker",
          "title": "Docker Community"
        },
        {
          "src": "/landing-2/logos/comp/logo-traefik-blue.svg",
          "alt": "logo do Traefik",
          "title": "Traefik Proxy"
        },
        {
          "src": "/landing-2/logos/comp/logo-postgres-blue.svg",
          "alt": "logo do PostgreSQL",
          "title": "PostgreSQL"
        },
        {
          "src": "/landing-2/logos/comp/logo-valkey-dark-blue.svg",
          "alt": "logo do Valkey",
          "title": "Valkey"
        },
        {
          "src": "/landing-2/logos/comp/logo-golang-blue.svg",
          "alt": "logo do Go",
          "title": "Go"
        },
        {
          "src": "/landing-2/logos/comp/logo-nodejs-green.svg",
          "alt": "logo do NodeJS",
          "title": "NodeJS"
        }
      ]
    }
  },
  {
    "id": "network-and-connect",
    "imgLight": "/landing-2/features/illustration-02-network--light.svg",
    "imgDark": "/landing-2/features/illustration-02-network--dark.svg",
    "alt": "Rede",
    "title": "Rede e conexão",
    "sub": "O Badblock oferece descoberta automática de serviços, rede muito rápida e suporte a qualquer protocolo, tudo pronto para usar.",
    "cards": [
      {
        "icon": {
          "kind": "feather",
          "name": "globe"
        },
        "hue": 272,
        "title": "Rede pública",
        "desc": "Exponha sua aplicação na internet pública.",
        "bullets": [
          {
            "icon": {
              "kind": "custom",
              "name": "custom-c31a4fcc"
            },
            "text": "Até 10 Gbps de velocidade de transferência"
          },
          {
            "icon": {
              "kind": "feather",
              "name": "check"
            },
            "text": "Domínio Badblock gratuito para todos os serviços"
          }
        ]
      },
      {
        "icon": {
          "kind": "feather",
          "name": "shield"
        },
        "hue": 272,
        "title": "Rede privada",
        "desc": "Conecte com segurança serviços no mesmo local por uma rede interna de alta velocidade.",
        "bullets": [
          {
            "icon": {
              "kind": "custom",
              "name": "custom-c31a4fcc"
            },
            "text": "Até 100 Gbps de velocidade de transferência"
          },
          {
            "icon": {
              "kind": "custom",
              "name": "custom-c31a4fcc"
            },
            "text": "Vários protocolos sobre IPv6"
          }
        ]
      },
      {
        "icon": {
          "kind": "custom",
          "name": "custom-e016bfdc"
        },
        "hue": 272,
        "title": "Proxy TCP",
        "desc": "Envie tráfego para serviços que não falam HTTP.",
        "bullets": [
          {
            "icon": {
              "kind": "feather",
              "name": "check"
            },
            "text": "Balanceamento de carga em L4"
          },
          {
            "icon": {
              "kind": "custom",
              "name": "custom-a5622aa0"
            },
            "text": "Porta configurável"
          }
        ]
      },
      {
        "icon": {
          "kind": "feather",
          "name": "link"
        },
        "hue": 272,
        "title": "Domínios próprios",
        "desc": "Use o domínio que quiser na sua aplicação.",
        "bullets": [
          {
            "icon": {
              "kind": "feather",
              "name": "check"
            },
            "text": "Suporte a domínios curinga (wildcard)"
          },
          {
            "icon": {
              "kind": "feather",
              "name": "check"
            },
            "text": "Domínios próprios ilimitados"
          }
        ]
      },
      {
        "icon": {
          "kind": "custom",
          "name": "custom-12aa0e9f"
        },
        "hue": 272,
        "title": "Gestão de certificados X.509",
        "desc": "Emissão gerenciada de certificados TLS para todos os domínios.",
        "bullets": [
          {
            "icon": {
              "kind": "feather",
              "name": "check"
            },
            "text": "Configuração em 3 cliques"
          },
          {
            "icon": {
              "kind": "feather",
              "name": "rotate-cw"
            },
            "text": "Renovação automática"
          }
        ]
      },
      {
        "icon": {
          "kind": "custom",
          "name": "custom-cc57179a"
        },
        "hue": 272,
        "title": "Proxy HTTP",
        "desc": "Termina o TLS perto dos seus usuários e protege sua aplicação.",
        "bullets": [
          {
            "icon": {
              "kind": "feather",
              "name": "check"
            },
            "text": "Balanceamento de carga em L4 e L7"
          },
          {
            "icon": {
              "kind": "feather",
              "name": "lock"
            },
            "text": "Proteção contra DDoS"
          }
        ]
      }
    ],
    "logoRow": {
      "label": "Alternativa a",
      "logos": [
        {
          "src": "/landing-2/logos/comp/logo-envoy.svg",
          "alt": "logo do Envoy",
          "title": "Envoy"
        },
        {
          "src": "/landing-2/logos/comp/logo-cilium.svg",
          "alt": "logo do Cilium",
          "title": "Cilium"
        },
        {
          "src": "/landing-2/logos/comp/logo-nginx.svg",
          "alt": "logo do Nginx",
          "title": "Nginx"
        },
        {
          "src": "/landing-2/logos/comp/logo-istio.svg",
          "alt": "logo do Istio",
          "title": "Istio"
        },
        {
          "src": "/landing-2/logos/comp/logo-haproxy.svg",
          "alt": "logo do Haproxy",
          "title": "Haproxy"
        }
      ]
    }
  },
  {
    "id": "scale-and-grow",
    "imgLight": "/landing-2/features/illustration-03-scale--light.svg",
    "imgDark": "/landing-2/features/illustration-03-scale--dark.svg",
    "alt": "Escalar",
    "title": "Escalar e crescer",
    "sub": "Na hora de escalar, o Badblock está à altura dos grandes provedores de nuvem: as máquinas mais rápidas, regiões no mundo todo e escala horizontal.",
    "cards": [
      {
        "icon": {
          "kind": "feather",
          "name": "maximize-2"
        },
        "hue": 208,
        "title": "Escala vertical",
        "desc": "Aumente CPU e memória automaticamente conforme a carga.",
        "bullets": [
          {
            "icon": {
              "kind": "feather",
              "name": "cpu"
            },
            "text": "Até 48 vCPU por réplica"
          },
          {
            "icon": {
              "kind": "custom",
              "name": "custom-6b143509"
            },
            "text": "Até 48 GB de memória por réplica"
          }
        ]
      },
      {
        "icon": {
          "kind": "feather",
          "name": "layers"
        },
        "hue": 208,
        "title": "Escala horizontal",
        "desc": "Adicione máquinas para absorver mais tráfego e carga sem atrito.",
        "bullets": [
          {
            "icon": {
              "kind": "feather",
              "name": "layers"
            },
            "text": "Até 50 réplicas por serviço"
          },
          {
            "icon": {
              "kind": "feather",
              "name": "rotate-ccw"
            },
            "text": "Balanceamento de carga automático"
          }
        ]
      },
      {
        "icon": {
          "kind": "feather",
          "name": "globe"
        },
        "hue": 208,
        "title": "Escala global",
        "desc": "Implante sua aplicação em qualquer uma de 4 regiões pelo mundo.",
        "bullets": [
          {
            "icon": {
              "kind": "feather",
              "name": "map-pin"
            },
            "text": "Estados Unidos (Leste, Oeste)"
          },
          {
            "icon": {
              "kind": "feather",
              "name": "map-pin"
            },
            "text": "Europa (Oeste)"
          },
          {
            "icon": {
              "kind": "feather",
              "name": "map-pin"
            },
            "text": "Sudeste Asiático"
          }
        ]
      }
    ],
    "logoRow": {
      "label": "Alternativa a",
      "logos": [
        {
          "src": "/landing-2/logos/comp/logo-kubernetes.svg",
          "alt": "logo do Kubernetes",
          "title": "Kubernetes"
        },
        {
          "src": "/landing-2/logos/comp/logo-amazon-ecs.svg",
          "alt": "logo do Amazon-ecs",
          "title": "Amazon-ecs"
        },
        {
          "src": "/landing-2/logos/comp/logo-nomad.svg",
          "alt": "logo do Nomad",
          "title": "Nomad"
        },
        {
          "src": "/landing-2/logos/comp/logo-betterstack.svg",
          "alt": "logo do Betterstack",
          "title": "Betterstack"
        }
      ]
    }
  },
  {
    "id": "monitor-and-observe",
    "imgLight": "/landing-2/features/illustration-04-monitor--light.svg",
    "imgDark": "/landing-2/features/illustration-04-monitor--dark.svg",
    "alt": "Monitorar",
    "title": "Monitorar e observar",
    "sub": "Parece que veio um pico de tráfego! Monitore suas aplicações com tranquilidade, com painéis de observabilidade totalmente configuráveis.",
    "cards": [
      {
        "icon": {
          "kind": "feather",
          "name": "align-left"
        },
        "hue": 192,
        "title": "Logs de build e implantação",
        "desc": "Filtre, consulte, encaminhe e compartilhe os logs de build e de implantação.",
        "bullets": [
          {
            "icon": {
              "kind": "feather",
              "name": "calendar"
            },
            "text": "Retenção de até 90 dias"
          },
          {
            "icon": {
              "kind": "feather",
              "name": "check"
            },
            "text": "Suporte a logs estruturados em JSON"
          }
        ]
      },
      {
        "icon": {
          "kind": "feather",
          "name": "bar-chart-2"
        },
        "hue": 192,
        "title": "Painéis configuráveis",
        "desc": "Acompanhe e crie alertas de uso de recursos e de desempenho.",
        "bullets": [
          {
            "icon": {
              "kind": "custom",
              "name": "custom-46e100a0"
            },
            "text": "CPU, RAM, disco, rede"
          },
          {
            "icon": {
              "kind": "feather",
              "name": "mouse-pointer"
            },
            "text": "Personalização por arrastar e soltar"
          }
        ]
      },
      {
        "icon": {
          "kind": "custom",
          "name": "custom-53969462"
        },
        "hue": 192,
        "title": "Webhooks",
        "desc": "Receba notificações por webhooks no Discord ou no Slack.",
        "bullets": [
          {
            "icon": {
              "kind": "feather",
              "name": "slack"
            },
            "text": "Compatível com Slack"
          },
          {
            "icon": {
              "kind": "custom",
              "name": "custom-ffbc323d"
            },
            "text": "Compatível com Discord"
          }
        ]
      },
      {
        "icon": {
          "kind": "feather",
          "name": "heart"
        },
        "hue": 192,
        "title": "Endpoint de healthcheck",
        "desc": "Personalize como o Badblock avalia as implantações.",
        "bullets": [
          {
            "icon": {
              "kind": "custom",
              "name": "custom-a5622aa0"
            },
            "text": "Configuração personalizada"
          },
          {
            "icon": {
              "kind": "custom",
              "name": "custom-3e10a870"
            },
            "text": "Timeouts configuráveis"
          }
        ]
      },
      {
        "icon": {
          "kind": "feather",
          "name": "alert-triangle"
        },
        "hue": 192,
        "title": "Limites de uso",
        "desc": "Defina limites para controlar o gasto com recursos no Badblock.",
        "bullets": [
          {
            "icon": {
              "kind": "feather",
              "name": "sliders"
            },
            "text": "Limites rígidos e flexíveis"
          },
          {
            "icon": {
              "kind": "feather",
              "name": "mail"
            },
            "text": "Alertas por e-mail"
          }
        ]
      },
      {
        "icon": {
          "kind": "feather",
          "name": "bar-chart-2"
        },
        "hue": 192,
        "title": "Alertas configuráveis",
        "desc": "Seja avisado quando uma métrica passar de um limite.",
        "bullets": [
          {
            "icon": {
              "kind": "custom",
              "name": "custom-46e100a0"
            },
            "text": "E-mails, webhooks, chamados de plantão"
          },
          {
            "icon": {
              "kind": "custom",
              "name": "custom-c3f87c81"
            },
            "text": "Baixa e alta urgência"
          }
        ]
      }
    ],
    "logoRow": {
      "label": "Alternativa a",
      "logos": [
        {
          "src": "/landing-2/logos/comp/logo-datadog.svg",
          "alt": "logo do Datadog",
          "title": "Datadog"
        },
        {
          "src": "/landing-2/logos/comp/logo-sentry.svg",
          "alt": "logo do Sentry",
          "title": "Sentry"
        },
        {
          "src": "/landing-2/logos/comp/logo-opentele.svg",
          "alt": "logo do Opentele",
          "title": "Opentele"
        }
      ]
    }
  },
  {
    "id": "improve-and-automate",
    "imgLight": "/landing-2/features/illustration-05-evolve--light.svg",
    "imgDark": "/landing-2/features/illustration-05-evolve--dark.svg",
    "alt": "Evoluir",
    "title": "Evoluir e colaborar",
    "sub": "O Badblock tem gestão completa do ciclo de vida e recursos de colaboração em equipe. Toda mudança pode ser preparada, pré-visualizada, mesclada ou revertida.",
    "cards": [
      {
        "icon": {
          "kind": "feather",
          "name": "layers"
        },
        "hue": 164,
        "title": "Ambientes",
        "desc": "Isole a stack inteira e introduza mudanças com segurança.",
        "bullets": [
          {
            "icon": {
              "kind": "feather",
              "name": "layers"
            },
            "text": "Ambientes ilimitados"
          },
          {
            "icon": {
              "kind": "feather",
              "name": "list"
            },
            "text": "Implantações seletivas"
          },
          {
            "icon": {
              "kind": "custom",
              "name": "custom-8edf95f7"
            },
            "text": "Sincronize e prepare mudanças"
          }
        ]
      },
      {
        "icon": {
          "kind": "custom",
          "name": "custom-7929a19e"
        },
        "hue": 164,
        "title": "Ambientes por PR",
        "desc": "Ambientes temporários, desfeitos quando o PR é mesclado.",
        "bullets": [
          {
            "icon": {
              "kind": "custom",
              "name": "custom-a5622aa0"
            },
            "text": "Criação e remoção automáticas"
          },
          {
            "icon": {
              "kind": "feather",
              "name": "git-branch"
            },
            "text": "Implantação por branch do GitHub"
          },
          {
            "icon": {
              "kind": "feather",
              "name": "eye"
            },
            "text": "Implantações de pré-visualização"
          }
        ]
      },
      {
        "icon": {
          "kind": "feather",
          "name": "rotate-ccw"
        },
        "hue": 164,
        "title": "Reversões",
        "desc": "Reverta uma implantação com um clique.",
        "bullets": [
          {
            "icon": {
              "kind": "custom",
              "name": "custom-17feed52"
            },
            "text": "Volte ao último estado bem-sucedido"
          },
          {
            "icon": {
              "kind": "feather",
              "name": "power"
            },
            "text": "Reimplante ou reinicie"
          },
          {
            "icon": {
              "kind": "custom",
              "name": "custom-5c400be8"
            },
            "text": "Política de reinício configurável"
          }
        ]
      },
      {
        "icon": {
          "kind": "feather",
          "name": "box"
        },
        "hue": 164,
        "title": "API",
        "desc": "Conecte-se à mesma API que move o console do Badblock.",
        "bullets": [
          {
            "icon": {
              "kind": "feather",
              "name": "check"
            },
            "text": "Mais de 100 métodos"
          },
          {
            "icon": {
              "kind": "custom",
              "name": "custom-76956f89"
            },
            "text": "Feita em GraphQL"
          }
        ]
      },
      {
        "icon": {
          "kind": "feather",
          "name": "terminal"
        },
        "hue": 164,
        "title": "CLI",
        "desc": "Orquestre aplicações pela linha de comando.",
        "bullets": [
          {
            "icon": {
              "kind": "custom",
              "name": "custom-a5622aa0"
            },
            "text": "Mais de 25 comandos"
          },
          {
            "icon": {
              "kind": "custom",
              "name": "custom-3c4ffe53"
            },
            "text": "Feita em Rust"
          }
        ]
      },
      {
        "icon": {
          "kind": "feather",
          "name": "file-text"
        },
        "hue": 164,
        "title": "Configuração como código",
        "desc": "Gerencie a configuração com facilidade a partir de um arquivo TOML ou JSON.",
        "bullets": [
          {
            "icon": {
              "kind": "feather",
              "name": "sliders"
            },
            "text": "Sobrepõe as configurações da interface"
          },
          {
            "icon": {
              "kind": "feather",
              "name": "cloud"
            },
            "text": "Gerencie implantações"
          }
        ]
      }
    ],
    "logoRow": {
      "label": "Alternativa a",
      "logos": [
        {
          "src": "/landing-2/logos/comp/logo-terraform.svg",
          "alt": "logo do Terraform",
          "title": "Terraform"
        },
        {
          "src": "/landing-2/logos/comp/logo-spacelift.svg",
          "alt": "logo do Spacelift",
          "title": "Spacelift"
        }
      ]
    }
  },
  {
    "id": "security-and-compliance",
    "imgLight": "/landing-2/features/illustration-06-security--light.svg",
    "imgDark": "/landing-2/features/illustration-06-security--dark.svg",
    "alt": "Segurança e conformidade",
    "title": "Segurança e conformidade",
    "sub": "O Badblock entrega a confiabilidade e a conformidade de que você precisa, com a experiência que sua equipe de desenvolvimento merece.",
    "cards": [
      {
        "icon": {
          "kind": "feather",
          "name": "users"
        },
        "hue": 164,
        "title": "SSO",
        "desc": "Autenticação única (single sign-on) para a equipe acessar sem atrito.",
        "bullets": [
          {
            "icon": {
              "kind": "feather",
              "name": "shield"
            },
            "text": "Autenticação SAML"
          },
          {
            "icon": {
              "kind": "feather",
              "name": "users"
            },
            "text": "Gestão de equipes"
          }
        ]
      },
      {
        "icon": {
          "kind": "custom",
          "name": "custom-38b99227"
        },
        "hue": 164,
        "title": "Logs de auditoria",
        "desc": "Acompanhe todas as mudanças e ações dos usuários com logs de auditoria completos.",
        "bullets": [
          {
            "icon": {
              "kind": "feather",
              "name": "activity"
            },
            "text": "Registro da atividade dos usuários"
          },
          {
            "icon": {
              "kind": "custom",
              "name": "custom-38b99227"
            },
            "text": "Histórico de eventos"
          }
        ]
      },
      {
        "icon": {
          "kind": "feather",
          "name": "cloud"
        },
        "hue": 164,
        "title": "Traga sua própria nuvem",
        "desc": "Implante o Badblock dentro do seu ambiente de nuvem, já endurecido em segurança.",
        "bullets": [
          {
            "icon": {
              "kind": "feather",
              "name": "server"
            },
            "text": "Implante na sua VPC"
          },
          {
            "icon": {
              "kind": "feather",
              "name": "cloud"
            },
            "text": "Use seus créditos de nuvem"
          }
        ]
      },
      {
        "icon": {
          "kind": "feather",
          "name": "shield"
        },
        "hue": 164,
        "title": "Rede zero-trust",
        "desc": "Rede privada protegida pelos princípios de zero-trust.",
        "bullets": [
          {
            "icon": {
              "kind": "feather",
              "name": "lock"
            },
            "text": "Conexões criptografadas"
          },
          {
            "icon": {
              "kind": "custom",
              "name": "custom-12aa0e9f"
            },
            "text": "Sem configuração manual de firewall"
          }
        ]
      },
      {
        "icon": {
          "kind": "feather",
          "name": "life-buoy"
        },
        "hue": 164,
        "title": "SLOs de suporte",
        "desc": "Suporte excepcional para disponibilidade e desempenho confiáveis.",
        "bullets": [
          {
            "icon": {
              "kind": "feather",
              "name": "activity"
            },
            "text": "Suporte 24/7"
          },
          {
            "icon": {
              "kind": "feather",
              "name": "check"
            },
            "text": "SLAs em contrato"
          }
        ]
      },
      {
        "icon": {
          "kind": "feather",
          "name": "server"
        },
        "hue": 164,
        "title": "VMs dedicadas",
        "desc": "Recursos isolados para picos de demanda em qualquer região de nuvem pública.",
        "bullets": [
          {
            "icon": {
              "kind": "feather",
              "name": "lock"
            },
            "text": "Isolamento de recursos"
          },
          {
            "icon": {
              "kind": "feather",
              "name": "server"
            },
            "text": "Hosts dedicados"
          }
        ]
      }
    ],
    "logoRow": {
      "label": "Alternativa a",
      "logos": [
        {
          "src": "/landing-2/logos/comp/logo-splunk.svg",
          "alt": "logo do Splunk",
          "title": "Splunk"
        },
        {
          "src": "/landing-2/logos/comp/logo-amazon-iam.svg",
          "alt": "logo do Amazon-iam",
          "title": "Amazon-iam"
        }
      ]
    }
  }
];
