import { Link } from "react-router"
import { CircleAlertIcon, PencilIcon, PlusIcon, Trash2Icon, WalletCardsIcon } from "lucide-react"

import { QueryState } from "@/components/query-state"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { ButtonGroup } from "@/components/ui/button-group"
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty"
import { Switch } from "@/components/ui/switch"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useChannels, useDeleteChannel, useToggleChannel } from "@/hooks/queries"
import type { Channel } from "@/lib/types"

export function ChannelsPage() {
  const { data, error, refetch } = useChannels()
  const toggle = useToggleChannel()

  return (
    <Card>
      <CardHeader>
        <CardTitle>支付渠道</CardTitle>
        <CardDescription>
          「支付方式标识」即商户下单时传入的 type，需与 new-api 充值方式中的 type 一致。配置修改后立即生效。
        </CardDescription>
        <CardAction>
          <Button asChild>
            <Link to="/channels/new">
              <PlusIcon />
              新建渠道
            </Link>
          </Button>
        </CardAction>
      </CardHeader>
      <CardContent>
        {!data ? (
          <QueryState error={error} onRetry={refetch} />
        ) : data.length === 0 ? (
          <Empty>
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <WalletCardsIcon />
              </EmptyMedia>
              <EmptyTitle>还没有支付渠道</EmptyTitle>
              <EmptyDescription>添加支付宝、微信支付、PayPal 或 Stripe 后，商户即可发起支付。</EmptyDescription>
            </EmptyHeader>
            <EmptyContent>
              <Button asChild>
                <Link to="/channels/new">
                  <PlusIcon />
                  新建渠道
                </Link>
              </Button>
            </EmptyContent>
          </Empty>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>名称</TableHead>
                <TableHead>支付方式标识</TableHead>
                <TableHead>驱动</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>启用</TableHead>
                <TableHead>操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {data.map((c) => (
                <TableRow key={c.id}>
                  <TableCell>{c.name}</TableCell>
                  <TableCell>
                    <Badge variant="outline">{c.type}</Badge>
                  </TableCell>
                  <TableCell>{c.driverTitle || c.driver}</TableCell>
                  <TableCell>
                    <ChannelHealth channel={c} />
                  </TableCell>
                  <TableCell>
                    <Switch
                      checked={c.enabled}
                      disabled={toggle.isPending}
                      onCheckedChange={(enabled) => toggle.mutate({ id: c.id, enabled })}
                      aria-label={`${c.enabled ? "停用" : "启用"}${c.name}`}
                    />
                  </TableCell>
                  <TableCell>
                    <ButtonGroup>
                      <Button variant="outline" size="sm" asChild>
                        <Link to={`/channels/${c.id}`}>
                          <PencilIcon />
                          编辑
                        </Link>
                      </Button>
                      <DeleteChannelButton channel={c} />
                    </ButtonGroup>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  )
}

function ChannelHealth({ channel }: { channel: Channel }) {
  if (channel.error) {
    return (
      <Tooltip>
        <TooltipTrigger asChild>
          <Badge variant="destructive">
            <CircleAlertIcon />
            初始化失败
          </Badge>
        </TooltipTrigger>
        <TooltipContent>{channel.error}</TooltipContent>
      </Tooltip>
    )
  }
  return channel.enabled ? <Badge>运行中</Badge> : <Badge variant="secondary">已停用</Badge>
}

function DeleteChannelButton({ channel }: { channel: Channel }) {
  const remove = useDeleteChannel()
  return (
    <AlertDialog>
      <AlertDialogTrigger asChild>
        <Button variant="outline" size="sm">
          <Trash2Icon />
          删除
        </Button>
      </AlertDialogTrigger>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>删除「{channel.name}」？</AlertDialogTitle>
          <AlertDialogDescription>
            删除后该渠道的待支付订单将无法再接收上游回调。如只是暂停收款，建议使用「停用」——停用后仍会处理存量订单的回调。
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>取消</AlertDialogCancel>
          <AlertDialogAction variant="destructive" onClick={() => remove.mutate(channel.id)}>
            删除
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
