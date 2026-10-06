import {ChevronLeft, ChevronRight, MoreHorizontal} from 'lucide-react'
import * as React from 'react'
import {useTranslation} from 'react-i18next'

import {ButtonProps, buttonVariants} from '@/components/ui/button'
import {cn} from '@/lib/utils'

const Pagination = ({className, ...props}: React.ComponentProps<'nav'>) => {
	const {t} = useTranslation()
	return (
		<nav
			role='navigation'
			aria-label={t('pagination.label')}
			className={cn('mx-auto flex w-full justify-center', className)}
			{...props}
		/>
	)
}

function PaginationContent({
	className,
	ref,
	...props
}: React.ComponentProps<'ul'> & {ref?: React.Ref<HTMLUListElement>}) {
	return <ul ref={ref} className={cn('flex flex-row items-center gap-1', className)} {...props} />
}

function PaginationItem({className, ref, ...props}: React.ComponentProps<'li'> & {ref?: React.Ref<HTMLLIElement>}) {
	return <li ref={ref} className={cn('flex h-7 w-7 items-center justify-center', className)} {...props} />
}

type PaginationLinkProps = {
	isActive?: boolean
} & Pick<ButtonProps, 'size'> &
	React.ComponentProps<'a'>

const PaginationLink = ({className, isActive, size = 'icon-only', ...props}: PaginationLinkProps) => (
	<a
		aria-current={isActive ? 'page' : undefined}
		className={cn(
			buttonVariants({
				variant: isActive ? 'primary' : 'default',
				size,
			}),
			'rounded-md',
			'h-7 w-7',
			className,
		)}
		{...props}
	/>
)

const PaginationPrevious = ({
	className,
	children,
	...props
}: React.ComponentProps<typeof PaginationLink> & {children?: React.ReactNode}) => {
	const {t} = useTranslation()
	return (
		<PaginationLink
			aria-label={t('pagination.previous')}
			size='default'
			className={cn('h-7 w-7', className)}
			{...props}
		>
			{children || (
				<>
					<ChevronLeft className='h-4 w-4' />
					<span>{t('pagination.previous')}</span>
				</>
			)}
		</PaginationLink>
	)
}

const PaginationNext = ({
	className,
	children,
	...props
}: React.ComponentProps<typeof PaginationLink> & {children?: React.ReactNode}) => {
	const {t} = useTranslation()
	return (
		<PaginationLink
			aria-label={t('pagination.next')}
			size='default'
			className={cn('gap-1 pr-2.5', className)}
			{...props}
		>
			{children || (
				<>
					<span>{t('pagination.next')}</span>
					<ChevronRight className='h-4 w-4' />
				</>
			)}
		</PaginationLink>
	)
}

const PaginationEllipsis = ({className, ...props}: React.ComponentProps<'span'>) => {
	const {t} = useTranslation()
	return (
		<span aria-hidden className={cn('flex h-9 w-9 items-center justify-center', className)} {...props}>
			<MoreHorizontal className='h-4 w-4' />
			<span className='sr-only'>{t('pagination.more')}</span>
		</span>
	)
}

export {
	Pagination,
	PaginationContent,
	PaginationLink,
	PaginationItem,
	PaginationPrevious,
	PaginationNext,
	PaginationEllipsis,
}
