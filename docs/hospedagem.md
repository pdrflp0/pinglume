# Hospedagem dos endpoints

Roteiro para colocar os endpoints no ar sem custo. Conferido em 3 de novembro de 2026. Telas de console mudam com frequência: se um nome de botão estiver diferente, procure pelo campo equivalente.

## Plano

| Rótulo | Provedor | Região | Custo esperado | Papel |
| --- | --- | --- | --- | --- |
| `sp-1` | Oracle Cloud Always Free | São Paulo (`sa-saopaulo-1`) | zero | perto dos testadores |
| `us-1` | Google Cloud, teste gratuito de 90 dias | Carolina do Sul (`us-east1`) | zero nos 90 dias | outro provedor e outro país |

Dois endpoints, não três. Um terceiro sem custo seria outra VM da Oracle em São Paulo, que não separa problema de provedor nem de região.

Nenhum domínio é necessário agora. O agente fixa o certificado de cada endpoint, então TCP, HTTPS e UDP funcionam só com IP. A sonda DNS de produção fica desligada até existir um domínio.

Porta 443 em TCP e UDP nos dois: é a porta com menos chance de bloqueio em redes de terceiros.

## O que pode custar dinheiro

- **Oracle:** não faça upgrade para Pay As You Go. A própria Oracle diz que o cartão só é cobrado se você pedir o upgrade. Pode aparecer uma pré-autorização pequena, que é devolvida.
- **Google:** sem ativar a conta paga, não há cobrança: o Google para os recursos no fim do teste. O IPv4 externo pode ser cobrado fora do crédito (US$ 0,005/h, cerca de US$ 3,60/mês; as fontes se contradizem), e nos 90 dias isso sai do crédito de US$ 300.
- **Decisão no dia 80 do Google:** veja o relatório de faturamento. Se o IPv4 aparecer como custo, deixe o teste acabar e `us-1` sai do ar sem cobrança. Só ative a conta paga se decidir pagar.

## 1. Chave SSH

No Mac:

```bash
ssh-keygen -t ed25519 -f ~/.ssh/pinglume -C ubuntu -N ""
```

```bash
pbcopy < ~/.ssh/pinglume.pub
```

O segundo comando copia a chave pública, que é o texto colado nos dois provedores. A chave privada nunca sai do Mac. O `-C ubuntu` importa: é ele que define o usuário no Google.

## 2. Oracle: conta

1. Abra <https://signup.oraclecloud.com>.
2. País **Brazil**, seu nome e e-mail. Confirme o e-mail em até 30 minutos.
3. Crie a senha e o nome da conta de nuvem.
4. Em **Home Region**, escolha **Brazil East (Sao Paulo)**. Isso não muda depois, e as VMs gratuitas só existem nessa região.
5. Preencha endereço, telefone e CPF (`xxxxxxxxx-xx`).
6. Adicione um cartão de crédito. Débito sem PIN de bandeira grande também é aceito.
7. Clique em **Start my free trial**.

## 3. Oracle: VM

1. Em <https://cloud.oracle.com>, entre e vá em **Compute → Instances → Create instance**.
2. Nome: `pinglume-sp-1`.
3. **Image:** Canonical Ubuntu 24.04.
4. **Shape:** Ampere, `VM.Standard.A1.Flex`, 1 OCPU e 6 GB. Confirme o selo **Always Free-eligible**.
5. Se der `Out of host capacity`, troque para `VM.Standard.E2.1.Micro` (1/8 de OCPU, 1 GB). É menor, mas basta; só registre isso, porque uma CPU tão pequena pode somar variação às medições.
6. **Networking:** crie uma VCN nova com sub-rede pública e deixe marcado **Automatically assign public IPv4 address**.
7. **Add SSH keys:** escolha **Paste public keys** e cole o conteúdo copiado no passo 1.
8. Deixe o disco no padrão e clique em **Create**.
9. Quando o estado for **Running**, anote o **Public IP address**.

## 4. Oracle: liberar a porta

1. Abra a VM e clique na sub-rede (**Subnet**).
2. Em **Security**, abra a **Default Security List**.
3. Em **Add Ingress Rules**, crie duas regras:

| Source CIDR | IP Protocol | Destination Port |
| --- | --- | --- |
| `0.0.0.0/0` | TCP | `443` |
| `0.0.0.0/0` | UDP | `443` |

O firewall interno da VM é aberto pelo instalador.

## 5. Google: conta e VM

1. Abra <https://console.cloud.google.com/freetrial>, entre com sua conta Google e aceite o teste gratuito (crédito de US$ 300 por 90 dias). Pede cartão. Não clique em **Activate** / **Upgrade** depois.
2. Em <https://console.cloud.google.com/billing/budgets>, crie um orçamento de US$ 1 com alerta por e-mail. O alerta só avisa; ele não bloqueia gastos.
3. Abra <https://shell.cloud.google.com> e rode, um de cada vez:

```bash
gcloud services enable compute.googleapis.com
```

```bash
gcloud compute addresses create pinglume-us-1 --region=us-east1 --network-tier=STANDARD
```

```bash
gcloud compute firewall-rules create pinglume-probe --allow=tcp:443,udp:443 --source-ranges=0.0.0.0/0
```

Troque `COLE_A_CHAVE` pelo conteúdo copiado no passo 1, mantendo as aspas:

```bash
gcloud compute instances create pinglume-us-1 --zone=us-east1-b --machine-type=e2-micro --image-family=ubuntu-2404-lts-amd64 --image-project=ubuntu-os-cloud --boot-disk-size=30GB --boot-disk-type=pd-standard --network-tier=STANDARD --address=pinglume-us-1 --metadata=ssh-keys="ubuntu:COLE_A_CHAVE"
```

```bash
gcloud compute addresses describe pinglume-us-1 --region=us-east1 --format='value(address)'
```

O último comando mostra o IP de `us-1`. Se o nome da imagem falhar, liste as disponíveis:

```bash
gcloud compute images list --project=ubuntu-os-cloud --filter="family~ubuntu-2404"
```

Por que esses parâmetros: `pd-standard` é o único tipo de disco na franquia gratuita, o e2-micro gratuito só existe em `us-west1`, `us-central1` e `us-east1`, e o nível de rede `STANDARD` tem 200 GiB/mês de saída sem custo, contra 1 GB/mês do padrão Premium. O IP reservado não muda se a VM reiniciar, o que importa porque ele fica embutido no agente.

## 6. Instalar o probe-server

Do Mac, na raiz do repositório, trocando os IPs:

```bash
deploy/deploy.sh sp-1 sa-saopaulo-1 IP_DA_ORACLE
```

```bash
deploy/deploy.sh us-1 us-east1 IP_DO_GOOGLE
```

O script compila o servidor para a arquitetura da VM, instala o serviço, abre o firewall interno, testa HTTPS e UDP a partir do Mac e grava o endpoint em `agent/internal/endpoints/production.json`. Pode ser repetido.

O certificado é criado na VM, vale 10 anos e a chave privada fica só nela, em `/var/lib/pinglume-probe`. Se a VM for recriada, o certificado muda e o agente precisa ser recompilado.

## 7. Publicar o agente

Com a lista preenchida, `make build` gera um agente que mede os endpoints de produção com um simples `pinglume run`.

## Depois de no ar

- **VM parada na Oracle:** a Oracle pode recuperar VM Always Free ociosa. Conta como ociosa quem fica, por 7 dias, com CPU no percentil 95 abaixo de 20%, rede abaixo de 20% e, nas A1, memória abaixo de 20%. Um probe-server é exatamente esse caso. Se a VM parar, clique em **Start** no console; o IP efêmero se mantém enquanto a VM existir. Não usamos carga artificial para escapar dessa regra.
- **Enquanto a VM estiver parada:** o agente registra timeouts em `sp-1`. Comparar com `us-1` é o que evita culpar a rede do testador.
- **Tráfego:** cerca de 0,2 MB/h por testador em cada endpoint, mais 0,43 MB/h do UDP (estimativa a partir das [medições](medicoes-recursos.md)). As franquias de 10 TB (Oracle) e 200 GiB (Google) sobram para os testes de descoberta.
- **Privacidade:** o probe-server não grava IP nem requisições. Os provedores podem registrar tráfego por conta própria.

## Domínio, quando a sonda DNS fizer falta

1. Grátis e incerto: <https://nic.eu.org>. Há relatos de espera de meses e de que parou de aceitar pedidos; não confirmei.
2. Pago: um `.com` a preço de custo, cerca de US$ 10/ano, em <https://www.cloudflare.com/products/registrar/>.

Depois é preciso delegar uma zona para os IPs, ligar `-dns` no probe-server e preencher `dns_zone` na lista. Esse caminho tem código e teste unitário, mas nunca rodou contra um domínio real.

## Fontes

- [Oracle: cadastro](https://docs.oracle.com/en-us/iaas/Content/GSG/Tasks/signingup_topic-Sign_Up_for_Free_Oracle_Cloud_Promotion.htm)
- [Oracle: recursos Always Free](https://docs.oracle.com/en-us/iaas/Content/FreeTier/freetier_topic-Always_Free_Resources.htm)
- [Oracle: firewall em imagens Ubuntu](https://blogs.oracle.com/developers/post/enabling-network-traffic-to-ubuntu-images-in-oracle-cloud-infrastructure)
- [Google: teste gratuito e franquia](https://docs.cloud.google.com/free/docs/free-cloud-features)
- [Google: preço dos níveis de rede](https://cloud.google.com/network-tiers/pricing)
- [Google: preço de IPv4 externo](https://cloud.google.com/vpc/pricing-announce-external-ips)
